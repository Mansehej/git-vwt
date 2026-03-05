import path from "path"
import crypto from "crypto"
import fs from "fs/promises"

import type { Plugin } from "@opencode-ai/plugin"
import { tool } from "@opencode-ai/plugin"

function truthyEnv(value: string | undefined): boolean {
  if (!value) return false
  switch (value.trim().toLowerCase()) {
    case "1":
    case "true":
    case "yes":
    case "on":
      return true
    default:
      return false
  }
}

function sanitizeWorkspaceComponent(value: string): string {
  return value.replace(/[^A-Za-z0-9._-]/g, "_")
}

function wsForSession(sessionID: string): string {
  return `opencode-${sanitizeWorkspaceComponent(sessionID)}`
}

function stripLeadingDotSlash(p: string): string {
  let out = p
  while (out.startsWith("./")) out = out.slice(2)
  return out
}

function toPosixPath(p: string): string {
  return p.split(path.sep).join("/")
}

function resolveWorktreePath(inputPath: string, directory: string, worktree: string, opts?: { allowRoot?: boolean }) {
  const trimmed = stripLeadingDotSlash(String(inputPath ?? "").trim())
  if (!trimmed) throw new Error("empty path")

  const abs = path.isAbsolute(trimmed) ? path.normalize(trimmed) : path.resolve(directory, trimmed)
  const relRaw = path.relative(worktree, abs)
  if (relRaw.startsWith("..") || path.isAbsolute(relRaw)) {
    throw new Error(`refusing path outside worktree: ${trimmed}`)
  }

  const rel = stripLeadingDotSlash(toPosixPath(relRaw))
  if (!rel) {
    if (opts?.allowRoot) return { abs, rel: "." }
    throw new Error(`refusing worktree root path: ${trimmed}`)
  }
  if (rel === ".git" || rel.startsWith(".git/")) {
    throw new Error("refusing path .git/**")
  }
  return { abs, rel }
}

function formatLineNumbered(text: string, offset?: number, limit?: number): string {
  const start = Math.max(1, Math.trunc(offset ?? 1))
  const maxLines = Math.max(1, Math.trunc(limit ?? 2000))

  const lines = text.split("\n")
  const end = Math.min(lines.length, start+maxLines-1)

  const out: string[] = []
  for (let i = start; i <= end; i++) {
    let line = lines[i - 1] ?? ""
    if (line.length > 2000) line = line.slice(0, 2000)
    out.push(`${i}: ${line}`)
  }
  return out.join("\n") + (out.length ? "\n" : "")
}

function sha1(text: string): string {
  return crypto.createHash("sha1").update(text).digest("hex")
}

function isGitVwtApply(command: string): boolean {
  const s = String(command ?? "")
  // Match even when nested in quotes (e.g. `bash -c "git vwt apply"`).
  if (/\bgit\s+vwt\b[^;&|\n]*\bapply\b/m.test(s)) return true
  if (/\bgit-vwt\b[^;&|\n]*\bapply\b/m.test(s)) return true
  return false
}

function vwtAuthor(agent: string): string {
  const a = String(agent ?? "").trim()
  return a ? `opencode:${a}` : "opencode"
}

const APPLY_PATCH_DESCRIPTION = `Use the \`apply_patch\` tool to edit files. Your patch language is a stripped-down, file-oriented diff format designed to be easy to parse and safe to apply. You can think of it as a high-level envelope:

*** Begin Patch
[ one or more file sections ]
*** End Patch

Within that envelope, you get a sequence of file operations.
You MUST include a header to specify the action you are taking.
Each operation starts with one of three headers:

*** Add File: <path> - create a new file. Every following line is a + line (the initial contents).
*** Delete File: <path> - remove an existing file. Nothing follows.
*** Update File: <path> - patch an existing file in place (optionally with a rename).

Example patch:

\`\`\`
*** Begin Patch
*** Add File: hello.txt
+Hello world
*** Update File: src/app.py
*** Move to: src/main.py
@@ def greet():
-print("Hi")
+print("Hello, world!")
*** Delete File: obsolete.txt
*** End Patch
\`\`\`

It is important to remember:

- You must include a header with your intended action (Add/Delete/Update)
- You must prefix new lines with \`+\` even when creating a new file`

const VWT_TITLE_PREFIX = "[VWT] "

type PatchHunk =
  | { type: "add"; path: string; contents: string }
  | { type: "delete"; path: string }
  | { type: "update"; path: string; move_path?: string; chunks: UpdateFileChunk[] }

type UpdateFileChunk = {
  old_lines: string[]
  new_lines: string[]
  change_context?: string
  is_end_of_file?: boolean
}

function stripHeredoc(input: string): string {
  // Match heredoc patterns like: cat <<'EOF'\n...\nEOF or <<EOF\n...\nEOF
  const heredocMatch = input.match(/^(?:cat\s+)?<<['"]?(\w+)['"]?\s*\n([\s\S]*?)\n\1\s*$/)
  if (heredocMatch) return heredocMatch[2]
  return input
}

function parsePatchHeader(
  lines: string[],
  startIdx: number,
): { filePath: string; movePath?: string; nextIdx: number } | null {
  const line = lines[startIdx]

  if (line.startsWith("*** Add File:")) {
    const filePath = line.slice("*** Add File:".length).trim()
    return filePath ? { filePath, nextIdx: startIdx + 1 } : null
  }

  if (line.startsWith("*** Delete File:")) {
    const filePath = line.slice("*** Delete File:".length).trim()
    return filePath ? { filePath, nextIdx: startIdx + 1 } : null
  }

  if (line.startsWith("*** Update File:")) {
    const filePath = line.slice("*** Update File:".length).trim()
    let movePath: string | undefined
    let nextIdx = startIdx + 1

    if (nextIdx < lines.length && lines[nextIdx].startsWith("*** Move to:")) {
      movePath = lines[nextIdx].slice("*** Move to:".length).trim()
      nextIdx++
    }

    return filePath ? { filePath, movePath, nextIdx } : null
  }

  return null
}

function parseUpdateFileChunks(lines: string[], startIdx: number): { chunks: UpdateFileChunk[]; nextIdx: number } {
  const chunks: UpdateFileChunk[] = []
  let i = startIdx

  while (i < lines.length && !lines[i].startsWith("***")) {
    if (!lines[i].startsWith("@@")) {
      i++
      continue
    }

    const contextLine = lines[i].substring(2).trim()
    i++

    const oldLines: string[] = []
    const newLines: string[] = []
    let isEndOfFile = false

    while (i < lines.length && !lines[i].startsWith("@@") && !lines[i].startsWith("***")) {
      const changeLine = lines[i]

      if (changeLine === "*** End of File") {
        isEndOfFile = true
        i++
        break
      }

      if (changeLine.startsWith(" ")) {
        const content = changeLine.substring(1)
        oldLines.push(content)
        newLines.push(content)
      } else if (changeLine.startsWith("-")) {
        oldLines.push(changeLine.substring(1))
      } else if (changeLine.startsWith("+")) {
        newLines.push(changeLine.substring(1))
      }

      i++
    }

    chunks.push({
      old_lines: oldLines,
      new_lines: newLines,
      change_context: contextLine || undefined,
      is_end_of_file: isEndOfFile || undefined,
    })
  }

  return { chunks, nextIdx: i }
}

function parseAddFileContent(lines: string[], startIdx: number): { content: string; nextIdx: number } {
  let content = ""
  let i = startIdx

  while (i < lines.length && !lines[i].startsWith("***")) {
    if (lines[i].startsWith("+")) {
      content += lines[i].substring(1) + "\n"
    }
    i++
  }

  if (content.endsWith("\n")) content = content.slice(0, -1)
  return { content, nextIdx: i }
}

function parsePatch(patchText: string): { hunks: PatchHunk[] } {
  const cleaned = stripHeredoc(patchText.trim())
  const lines = cleaned.split("\n")
  const hunks: PatchHunk[] = []

  const beginMarker = "*** Begin Patch"
  const endMarker = "*** End Patch"
  const beginIdx = lines.findIndex((line) => line.trim() === beginMarker)
  const endIdx = lines.findIndex((line) => line.trim() === endMarker)
  if (beginIdx === -1 || endIdx === -1 || beginIdx >= endIdx) {
    throw new Error("Invalid patch format: missing Begin/End markers")
  }

  let i = beginIdx + 1
  while (i < endIdx) {
    const header = parsePatchHeader(lines, i)
    if (!header) {
      i++
      continue
    }

    if (lines[i].startsWith("*** Add File:")) {
      const { content, nextIdx } = parseAddFileContent(lines, header.nextIdx)
      hunks.push({ type: "add", path: header.filePath, contents: content })
      i = nextIdx
      continue
    }

    if (lines[i].startsWith("*** Delete File:")) {
      hunks.push({ type: "delete", path: header.filePath })
      i = header.nextIdx
      continue
    }

    if (lines[i].startsWith("*** Update File:")) {
      const { chunks, nextIdx } = parseUpdateFileChunks(lines, header.nextIdx)
      hunks.push({ type: "update", path: header.filePath, move_path: header.movePath, chunks })
      i = nextIdx
      continue
    }

    i++
  }

  return { hunks }
}

function normalizeUnicode(str: string): string {
  return str
    .replace(/[\u2018\u2019\u201A\u201B]/g, "'")
    .replace(/[\u201C\u201D\u201E\u201F]/g, '"')
    .replace(/[\u2010\u2011\u2012\u2013\u2014\u2015]/g, "-")
    .replace(/\u2026/g, "...")
    .replace(/\u00A0/g, " ")
}

type Comparator = (a: string, b: string) => boolean

function tryMatch(lines: string[], pattern: string[], startIndex: number, compare: Comparator, eof: boolean): number {
  if (eof) {
    const fromEnd = lines.length - pattern.length
    if (fromEnd >= startIndex) {
      let matches = true
      for (let j = 0; j < pattern.length; j++) {
        if (!compare(lines[fromEnd + j], pattern[j])) {
          matches = false
          break
        }
      }
      if (matches) return fromEnd
    }
  }

  for (let i = startIndex; i <= lines.length - pattern.length; i++) {
    let matches = true
    for (let j = 0; j < pattern.length; j++) {
      if (!compare(lines[i + j], pattern[j])) {
        matches = false
        break
      }
    }
    if (matches) return i
  }

  return -1
}

function seekSequence(lines: string[], pattern: string[], startIndex: number, eof = false): number {
  if (pattern.length === 0) return -1

  const exact = tryMatch(lines, pattern, startIndex, (a, b) => a === b, eof)
  if (exact !== -1) return exact

  const rstrip = tryMatch(lines, pattern, startIndex, (a, b) => a.trimEnd() === b.trimEnd(), eof)
  if (rstrip !== -1) return rstrip

  const trim = tryMatch(lines, pattern, startIndex, (a, b) => a.trim() === b.trim(), eof)
  if (trim !== -1) return trim

  return tryMatch(
    lines,
    pattern,
    startIndex,
    (a, b) => normalizeUnicode(a.trim()) === normalizeUnicode(b.trim()),
    eof,
  )
}

function computeReplacements(
  originalLines: string[],
  filePathForErrors: string,
  chunks: UpdateFileChunk[],
): Array<[number, number, string[]]> {
  const replacements: Array<[number, number, string[]]> = []
  let lineIndex = 0

  for (const chunk of chunks) {
    if (chunk.change_context) {
      const contextIdx = seekSequence(originalLines, [chunk.change_context], lineIndex)
      if (contextIdx === -1) {
        throw new Error(`Failed to find context '${chunk.change_context}' in ${filePathForErrors}`)
      }
      lineIndex = contextIdx + 1
    }

    if (chunk.old_lines.length === 0) {
      replacements.push([originalLines.length, 0, chunk.new_lines])
      continue
    }

    let pattern = chunk.old_lines
    let newSlice = chunk.new_lines
    let found = seekSequence(originalLines, pattern, lineIndex, chunk.is_end_of_file)

    if (found === -1 && pattern.length > 0 && pattern[pattern.length - 1] === "") {
      pattern = pattern.slice(0, -1)
      if (newSlice.length > 0 && newSlice[newSlice.length - 1] === "") {
        newSlice = newSlice.slice(0, -1)
      }
      found = seekSequence(originalLines, pattern, lineIndex, chunk.is_end_of_file)
    }

    if (found === -1) {
      throw new Error(`Failed to find expected lines in ${filePathForErrors}:\n${chunk.old_lines.join("\n")}`)
    }

    replacements.push([found, pattern.length, newSlice])
    lineIndex = found + pattern.length
  }

  replacements.sort((a, b) => a[0] - b[0])
  return replacements
}

function applyReplacements(lines: string[], replacements: Array<[number, number, string[]]>): string[] {
  const result = [...lines]
  for (let i = replacements.length - 1; i >= 0; i--) {
    const [startIdx, oldLen, newSegment] = replacements[i]
    result.splice(startIdx, oldLen)
    for (let j = 0; j < newSegment.length; j++) {
      result.splice(startIdx + j, 0, newSegment[j])
    }
  }
  return result
}

function deriveNewContentsFromChunks(
  originalContent: string,
  filePathForErrors: string,
  chunks: UpdateFileChunk[],
): string {
  let originalLines = originalContent.split("\n")
  if (originalLines.length > 0 && originalLines[originalLines.length - 1] === "") {
    originalLines = originalLines.slice(0, -1)
  }

  const replacements = computeReplacements(originalLines, filePathForErrors, chunks)
  let newLines = applyReplacements(originalLines, replacements)

  if (newLines.length === 0 || newLines[newLines.length - 1] !== "") {
    newLines = [...newLines, ""]
  }

  return newLines.join("\n")
}

export const VwtModePlugin: Plugin = async ({ client, $, worktree: projectWorktree }) => {
  // ENV-variable toggle (per-run): this plugin is a no-op unless enabled.
  const envEnabled = truthyEnv(process.env.OPENCODE_VWT)
  if (!envEnabled) {
    return {}
  }

  let activeToastShown = false

  async function showVwtActiveToast(): Promise<void> {
    if (activeToastShown) return
    activeToastShown = true
    await client.tui
      .showToast({
        body: {
          title: "VWT mode active",
          message:
            "OPENCODE_VWT=1 is set. Subagents edit isolated git-vwt workspaces; the primary edits the working tree normally.",
          variant: "info",
          duration: 12000,
        },
      })
      .catch(() => {})
  }

  async function ensureSessionTitle(sessionID: string, title: string | undefined): Promise<void> {
    const t = String(title ?? "").trim()
    if (!t) return
    if (t.startsWith(VWT_TITLE_PREFIX)) return
    await client.session
      .update({
        path: { id: sessionID },
        body: { title: VWT_TITLE_PREFIX + t },
      })
      .catch(() => {})
  }

  const parentBySession = new Map<string, string | null>()
  const openedWorkspaces = new Set<string>()
  const lastNotifiedPatchHashBySession = new Map<string, string>()

  let vwtPrefixCache: string[] | null = null

  async function vwtPrefix(cwd: string): Promise<string[]> {
    if (vwtPrefixCache) return vwtPrefixCache

    // Prefer `git vwt` only if it looks like the *workspace* CLI.
    const helpRes = await $.cwd(cwd)`git vwt --help`.nothrow().quiet()
    const help = helpRes.stdout.toString() + helpRes.stderr.toString()
    if (help.includes("virtual workspace (no hunks") || help.includes("git vwt open")) {
      vwtPrefixCache = ["git", "vwt"]
      return vwtPrefixCache
    }

    // Fallback to a repo-local build (`go build -o git-vwt ./cmd/git-vwt`).
    for (const candidate of [path.join(cwd, "git-vwt"), path.join(cwd, "git-vwt.exe")]) {
      try {
        await fs.access(candidate)
        vwtPrefixCache = [candidate]
        return vwtPrefixCache
      } catch {
        // ignore
      }
    }

    throw new Error(
      "could not find the git-vwt workspace CLI. Expected `git vwt` to support open/read/write, or a built `./git-vwt` in the repo root (go build -o git-vwt ./cmd/git-vwt).",
    )
  }

  async function getParentID(sessionID: string): Promise<string | null> {
    if (parentBySession.has(sessionID)) return parentBySession.get(sessionID) ?? null
    try {
      const res: any = await client.session.get({ path: { id: sessionID } })
      const info = res?.data ?? res
      const parentID = info?.parentID ?? null
      parentBySession.set(sessionID, parentID)
      return parentID
    } catch {
      parentBySession.set(sessionID, null)
      return null
    }
  }

  async function isChildSession(sessionID: string): Promise<boolean> {
    return (await getParentID(sessionID)) != null
  }

  async function ensureWsOpen(ws: string, agent: string, cwd: string): Promise<void> {
    if (openedWorkspaces.has(ws)) return
    try {
      const prefix = await vwtPrefix(cwd)
      await $.cwd(cwd)`${prefix} --ws ${ws} --agent ${agent} open`.quiet()
      openedWorkspaces.add(ws)
    } catch (err: any) {
      const stderr = err?.stderr?.toString?.() ?? ""
      const hint =
        stderr.includes("is not a git command") || stderr.includes("unknown subcommand")
          ? "\nHint: ensure you're using the workspace CLI (this repo's ./git-vwt), not an older `git vwt` install."
          : ""
      throw new Error(`failed to open VWT workspace ${ws}: ${err?.message ?? String(err)}${hint}`)
    }
  }

  async function vwtInfo(ws: string, agent: string, cwd: string): Promise<{ name: string; head: string; base: string }> {
    await ensureWsOpen(ws, agent, cwd)
    const prefix = await vwtPrefix(cwd)
    const out = await $.cwd(cwd)`${prefix} --ws ${ws} --agent ${agent} info`.text()
    const toks = out.trim().split(/\s+/)
    if (toks.length < 3) throw new Error(`unexpected git vwt info output: ${out.trim()}`)
    return { name: toks[0], head: toks[1], base: toks[2] }
  }

  async function vwtRead(ws: string, agent: string, cwd: string, relPath: string): Promise<string> {
    await ensureWsOpen(ws, agent, cwd)
    const prefix = await vwtPrefix(cwd)
    return await $.cwd(cwd)`${prefix} --ws ${ws} --agent ${agent} read ${relPath}`.text()
  }

  async function vwtWrite(ws: string, agent: string, cwd: string, relPath: string, content: string): Promise<string> {
    await ensureWsOpen(ws, agent, cwd)
    const input = new Response(content)
    const prefix = await vwtPrefix(cwd)
    return await $.cwd(cwd)`${prefix} --ws ${ws} --agent ${agent} write ${relPath} < ${input}`.text()
  }

  async function vwtList(ws: string, agent: string, cwd: string, relPath: string): Promise<string> {
    await ensureWsOpen(ws, agent, cwd)
    const arg = relPath === "." ? [] : [relPath]
    const prefix = await vwtPrefix(cwd)
    return await $.cwd(cwd)`${prefix} --ws ${ws} --agent ${agent} ls ${arg}`.text()
  }

  async function vwtSearch(ws: string, agent: string, cwd: string, pattern: string, pathspec: string[]): Promise<string> {
    await ensureWsOpen(ws, agent, cwd)
    const prefix = await vwtPrefix(cwd)
    const extra = pathspec.length ? (["--", ...pathspec] as const) : ([] as const)
    return await $.cwd(cwd)`${prefix} --ws ${ws} --agent ${agent} search ${pattern} ${extra}`.text()
  }

  async function vwtRemove(ws: string, agent: string, cwd: string, relPath: string): Promise<string> {
    await ensureWsOpen(ws, agent, cwd)
    const prefix = await vwtPrefix(cwd)
    return await $.cwd(cwd)`${prefix} --ws ${ws} --agent ${agent} rm ${relPath}`.text()
  }

  async function vwtMove(ws: string, agent: string, cwd: string, fromRel: string, toRel: string): Promise<string> {
    await ensureWsOpen(ws, agent, cwd)
    const prefix = await vwtPrefix(cwd)
    return await $.cwd(cwd)`${prefix} --ws ${ws} --agent ${agent} mv ${fromRel} ${toRel}`.text()
  }

  async function vwtPatch(ws: string, agent: string, cwd: string): Promise<string> {
    await ensureWsOpen(ws, agent, cwd)
    const prefix = await vwtPrefix(cwd)
    return await $.cwd(cwd)`${prefix} --ws ${ws} --agent ${agent} patch`.text()
  }

  async function vwtApply(
    ws: string,
    agent: string,
    cwd: string,
  ): Promise<{ exitCode: number; stdout: string; stderr: string }> {
    await ensureWsOpen(ws, agent, cwd)
    const prefix = await vwtPrefix(cwd)
    const res = await $.cwd(cwd)`${prefix} --ws ${ws} --agent ${agent} apply`.nothrow().quiet()
    return {
      exitCode: res.exitCode,
      stdout: res.stdout.toString(),
      stderr: res.stderr.toString(),
    }
  }

  return {
    async "chat.message"(input) {
      // Fallback: if we're continuing an existing session (no session.created event),
      // show the indicator toast on the first message.
      if (input.sessionID) {
        await showVwtActiveToast()
      }
    },

    async event({ event }) {
      if (event.type === "session.created") {
        const info: any = (event as any).properties?.info
        if (info?.id) parentBySession.set(info.id, info.parentID ?? null)
        if (info?.id && !info?.parentID) {
          await showVwtActiveToast()
        }
        if (info?.id) {
          await ensureSessionTitle(info.id, info.title)
        }
        return
      }

      if (event.type === "session.updated") {
        const info: any = (event as any).properties?.info
        if (info?.id) {
          await ensureSessionTitle(info.id, info.title)
        }
        return
      }
      if (event.type === "session.deleted") {
        const info: any = (event as any).properties?.info
        if (info?.id) {
          parentBySession.delete(info.id)
          lastNotifiedPatchHashBySession.delete(info.id)
        }
        return
      }
      if (event.type !== "session.idle") return

      const sessionID: string | undefined = (event as any).properties?.sessionID
      if (!sessionID) return
      if (!(await isChildSession(sessionID))) return

      const ws = wsForSession(sessionID)
      if (!openedWorkspaces.has(ws)) return

      let patch = ""
      try {
        patch = await vwtPatch(ws, "opencode", projectWorktree)
      } catch {
        return
      }
      if (!patch.trim()) return

      const digest = sha1(patch)
      if (lastNotifiedPatchHashBySession.get(sessionID) === digest) return
      lastNotifiedPatchHashBySession.set(sessionID, digest)

      await client.tui.showToast({
        body: {
          title: "VWT patch ready",
          message: `Subagent session ${sessionID} has workspace changes: ${ws}. Use: ./git-vwt --ws ${ws} patch (or git vwt --ws ${ws} patch if installed)`,
          variant: "info",
          duration: 8000,
        },
      })
    },

    async "experimental.chat.system.transform"(input, output) {
      const ws = wsForSession(input.sessionID)
      output.system.push(
        [
          "VWT mode is enabled (OPENCODE_VWT=1).",
          "- Primary session: file tools edit the working directory (normal OpenCode behavior).",
          `- Subagent sessions: file tools edit an isolated git-vwt workspace named opencode-<sessionID> (this session's workspace would be ${ws}).`,
          "- Subagents must never apply changes to the working directory.",
          "- To review/apply a subagent workspace from the primary, use vwt_patch/vwt_apply with that subagent sessionID (primary should call vwt_apply, not ask the user to run it).",
          "- If vwt_apply reports conflicts, resolve conflict markers (<<<<<<< >>>>>>>) in the affected files.",
        ].join("\n"),
      )
    },

    async "tool.execute.before"(input, output) {
      if (input.tool === "bash") {
        const command = String(output.args?.command ?? "")
        if (isGitVwtApply(command) && (await isChildSession(input.sessionID))) {
          const ws = wsForSession(input.sessionID)
          throw new Error(
            `subagents can't apply. Ask the primary to run: ./git-vwt --ws ${ws} patch (or use vwt_patch/vwt_apply from the primary).`,
          )
        }
        return
      }
    },

    async "shell.env"(input, output) {
      if (!input.sessionID) return
      if (!(await isChildSession(input.sessionID))) return
      const ws = wsForSession(input.sessionID)
      output.env.VWT_WORKSPACE = ws
      output.env.VWT_AGENT = "opencode"
    },

    tool: {
      read: tool({
        description: "Read a file (VWT-aware when enabled).",
        args: {
          filePath: tool.schema.string().describe("Path to the file"),
          offset: tool.schema.number().int().optional().describe("Line offset (1-indexed)"),
          limit: tool.schema.number().int().optional().describe("Max lines"),
        },
        async execute(args, context) {
          const { abs, rel } = resolveWorktreePath(args.filePath, context.directory, context.worktree, { allowRoot: true })
          await context.ask({
            permission: "read",
            patterns: [rel],
            always: [rel],
            metadata: { path: rel },
          })

          const useVwt = await isChildSession(context.sessionID)
          if (!useVwt) {
            try {
              const st = await fs.stat(abs)
              if (st.isDirectory()) {
                const entries = await fs.readdir(abs, { withFileTypes: true })
                const lines = entries
                  .map((e) => (e.isDirectory() ? `${e.name}/` : e.name))
                  .sort((a, b) => a.localeCompare(b))
                const start = Math.max(0, Math.trunc((args.offset ?? 1) - 1))
                const limit = Math.max(1, Math.trunc(args.limit ?? 2000))
                const sliced = lines.slice(start, start + limit)
                return sliced.join("\n") + (sliced.length ? "\n" : "")
              }

              const content = await fs.readFile(abs, "utf8")
              return formatLineNumbered(content, args.offset, args.limit)
            } catch (err: any) {
              throw new Error(err?.message ?? String(err))
            }
          }

          const ws = wsForSession(context.sessionID)
          const author = vwtAuthor(context.agent)

          if (rel === ".") {
            return await vwtList(ws, author, context.worktree, rel)
          }

          try {
            const content = await vwtRead(ws, author, context.worktree, rel)
            return formatLineNumbered(content, args.offset, args.limit)
          } catch (err: any) {
            try {
              return await vwtList(ws, author, context.worktree, rel)
            } catch {
              throw err
            }
          }
        },
      }),

      write: tool({
        description: "Write a file (VWT-aware when enabled).",
        args: {
          filePath: tool.schema.string().describe("Path to the file"),
          content: tool.schema.string().describe("Full file content"),
        },
        async execute(args, context) {
          const { abs, rel } = resolveWorktreePath(args.filePath, context.directory, context.worktree)
          await context.ask({
            permission: "edit",
            patterns: [rel],
            always: [rel],
            metadata: { path: rel },
          })

          const useVwt = await isChildSession(context.sessionID)
          if (useVwt) {
            const ws = wsForSession(context.sessionID)
            await vwtWrite(ws, vwtAuthor(context.agent), context.worktree, rel, args.content)
            return "Wrote file successfully.\n"
          }

          await fs.mkdir(path.dirname(abs), { recursive: true })
          await fs.writeFile(abs, args.content, "utf8")
          return "Wrote file successfully.\n"
        },
      }),

      edit: tool({
        description: "Edit a file by string replacement (VWT-aware when enabled).",
        args: {
          filePath: tool.schema.string().describe("Path to the file"),
          oldString: tool.schema.string().describe("The text to replace"),
          newString: tool.schema.string().describe("The text to replace it with (must be different from oldString)"),
          replaceAll: tool.schema.boolean().optional().describe("Replace all occurrences of oldString (default false)"),
        },
        async execute(args, context) {
          if (args.oldString === args.newString) {
            throw new Error("No changes to apply: oldString and newString are identical.")
          }

          const { abs, rel } = resolveWorktreePath(args.filePath, context.directory, context.worktree)
          await context.ask({
            permission: "edit",
            patterns: [rel],
            always: [rel],
            metadata: { path: rel },
          })

          const useVwt = await isChildSession(context.sessionID)
          const ws = wsForSession(context.sessionID)
          const author = vwtAuthor(context.agent)

          let before = ""
          if (useVwt) {
            if (args.oldString !== "") {
              before = await vwtRead(ws, author, context.worktree, rel)
            }
          } else {
            try {
              before = await fs.readFile(abs, "utf8")
            } catch (err: any) {
              if (args.oldString !== "") throw err
              before = ""
            }
          }

          let after = ""
          if (args.oldString === "") {
            after = args.newString
          } else if (args.replaceAll) {
            if (!before.includes(args.oldString)) {
              throw new Error("Could not find oldString in the file.")
            }
            after = before.split(args.oldString).join(args.newString)
          } else {
            const first = before.indexOf(args.oldString)
            if (first < 0) throw new Error("Could not find oldString in the file.")
            const second = before.indexOf(args.oldString, first + args.oldString.length)
            if (second >= 0) {
              throw new Error("Found multiple matches for oldString. Provide more surrounding context or set replaceAll.")
            }
            after = before.slice(0, first) + args.newString + before.slice(first + args.oldString.length)
          }

          if (useVwt) {
            await vwtWrite(ws, author, context.worktree, rel, after)
            return "Edit applied successfully.\n"
          }

          await fs.mkdir(path.dirname(abs), { recursive: true })
          await fs.writeFile(abs, after, "utf8")
          return "Edit applied successfully.\n"
        },
      }),

      apply_patch: tool({
        description: APPLY_PATCH_DESCRIPTION,
        args: {
          patchText: tool.schema.string().describe("The full patch text that describes all changes to be made"),
        },
        async execute(args, context) {
          const raw = String(args.patchText ?? "")
          if (!raw.trim()) throw new Error("patchText is required")

          const patchText = raw.replace(/\r\n/g, "\n").replace(/\r/g, "\n")

          let hunks: PatchHunk[]
          try {
            hunks = parsePatch(patchText).hunks
          } catch (err: any) {
            throw new Error(`apply_patch verification failed: ${err?.message ?? String(err)}`)
          }

          if (hunks.length === 0) {
            const normalized = patchText.trim()
            if (normalized === "*** Begin Patch\n*** End Patch") {
              throw new Error("patch rejected: empty patch")
            }
            throw new Error("apply_patch verification failed: no hunks found")
          }

          const useVwt = await isChildSession(context.sessionID)
          const ws = wsForSession(context.sessionID)
          const author = vwtAuthor(context.agent)

          const touched: string[] = []
          for (const hunk of hunks) {
            const { rel } = resolveWorktreePath(hunk.path, context.directory, context.worktree)
            touched.push(rel)
            if (hunk.type === "update" && hunk.move_path) {
              const { rel: moveRel } = resolveWorktreePath(hunk.move_path, context.directory, context.worktree)
              touched.push(moveRel)
            }
          }
          const uniqueTouched = Array.from(new Set(touched)).sort((a, b) => a.localeCompare(b))

          await context.ask({
            permission: "edit",
            patterns: uniqueTouched,
            always: ["*"],
            metadata: { workspace: ws, files: uniqueTouched.join(", ") },
          })

          const summaryLines: string[] = []
          for (const hunk of hunks) {
            if (hunk.type === "add") {
              const { abs, rel } = resolveWorktreePath(hunk.path, context.directory, context.worktree)
              const newContent =
                hunk.contents.length === 0 || hunk.contents.endsWith("\n") ? hunk.contents : `${hunk.contents}\n`
              if (useVwt) {
                await vwtWrite(ws, author, context.worktree, rel, newContent)
              } else {
                await fs.mkdir(path.dirname(abs), { recursive: true })
                await fs.writeFile(abs, newContent, "utf8")
              }
              summaryLines.push(`A ${rel}`)
              continue
            }

            if (hunk.type === "delete") {
              const { abs, rel } = resolveWorktreePath(hunk.path, context.directory, context.worktree)
              if (useVwt) {
                await vwtRemove(ws, author, context.worktree, rel)
              } else {
                await fs.unlink(abs)
              }
              summaryLines.push(`D ${rel}`)
              continue
            }

            const { abs, rel } = resolveWorktreePath(hunk.path, context.directory, context.worktree)
            const before = useVwt
              ? await vwtRead(ws, author, context.worktree, rel)
              : await fs.readFile(abs, "utf8")
            const after = deriveNewContentsFromChunks(before, rel, hunk.chunks)

            if (hunk.move_path) {
              const { abs: moveAbs, rel: moveRel } = resolveWorktreePath(hunk.move_path, context.directory, context.worktree)
              if (useVwt) {
                await vwtWrite(ws, author, context.worktree, moveRel, after)
                if (moveRel !== rel) {
                  await vwtRemove(ws, author, context.worktree, rel)
                }
              } else {
                await fs.mkdir(path.dirname(moveAbs), { recursive: true })
                await fs.writeFile(moveAbs, after, "utf8")
                if (moveAbs !== abs) {
                  await fs.unlink(abs)
                }
              }
              summaryLines.push(`M ${moveRel}`)
              continue
            }

            if (useVwt) {
              await vwtWrite(ws, author, context.worktree, rel, after)
            } else {
              await fs.writeFile(abs, after, "utf8")
            }
            summaryLines.push(`M ${rel}`)
          }

          if (useVwt) {
            return `Success. Updated the following files in workspace ${ws}:\n${summaryLines.join("\n")}\n`
          }
          return `Success. Updated the following files:\n${summaryLines.join("\n")}\n`
        },
      }),

      grep: tool({
        description: "Search file contents using a regex (VWT-aware when enabled).",
        args: {
          pattern: tool.schema.string().describe("Regex pattern"),
          path: tool.schema.string().optional().describe("Optional directory path to search (relative)"),
          include: tool.schema.string().optional().describe("Optional file glob filter (ripgrep --glob)") ,
        },
        async execute(args, context) {
          await context.ask({
            permission: "grep",
            patterns: [args.pattern],
            always: [args.pattern],
            metadata: { pattern: args.pattern },
          })

          const useVwt = await isChildSession(context.sessionID)
          if (!useVwt) {
            const cwd = context.worktree
            const searchPath = args.path
              ? resolveWorktreePath(args.path, context.directory, context.worktree, { allowRoot: true }).abs
              : cwd

            const extra: string[] = []
            if (args.include) extra.push("--glob", args.include)

            const res = await $.cwd(cwd)`rg --no-heading --line-number --hidden --no-messages --color never ${extra} ${args.pattern} ${searchPath}`
              .nothrow()
              .quiet()

            if (res.exitCode === 0) return res.text()
            if (res.exitCode === 1) return ""
            if (res.exitCode === 2 && res.stdout.toString().trim()) return res.text()
            throw new Error(res.stderr.toString())
          }

          const ws = wsForSession(context.sessionID)

          const baseRel = args.path
            ? resolveWorktreePath(args.path, context.directory, context.worktree, { allowRoot: true }).rel
            : "."

          const pathspec: string[] = []
          const rawInclude = String(args.include ?? "").trim()
          if (rawInclude) {
            if (rawInclude.startsWith(":(")) {
              pathspec.push(rawInclude)
            } else {
              const incNoSlash = rawInclude.replace(/^\/+/, "")
              let includePath = baseRel !== "." ? `${baseRel}/${incNoSlash}` : incNoSlash
              if (!includePath.includes("/")) includePath = `**/${includePath}`
              pathspec.push(`:(glob)${includePath}`)
            }
          } else if (baseRel !== ".") {
            pathspec.push(baseRel)
          }

          return await vwtSearch(ws, vwtAuthor(context.agent), context.worktree, args.pattern, pathspec)
        },
      }),

      list: tool({
        description: "List directory contents (VWT-aware when enabled).",
        args: {
          path: tool.schema.string().optional().describe("Directory path"),
        },
        async execute(args, context) {
          const target = args.path ?? "."
          const { abs, rel } = resolveWorktreePath(target, context.directory, context.worktree, { allowRoot: true })
          await context.ask({
            permission: "list",
            patterns: [rel],
            always: [rel],
            metadata: { path: rel },
          })

          const useVwt = await isChildSession(context.sessionID)
          if (useVwt) {
            return await vwtList(wsForSession(context.sessionID), vwtAuthor(context.agent), context.worktree, rel)
          }

          const entries = await fs.readdir(abs, { withFileTypes: true })
          const lines = entries
            .map((e) => (e.isDirectory() ? `${e.name}/` : e.name))
            .sort((a, b) => a.localeCompare(b))
          return lines.join("\n") + (lines.length ? "\n" : "")
        },
      }),

      glob: tool({
        description: "Find files by glob pattern (VWT-aware when enabled).",
        args: {
          pattern: tool.schema.string().describe("Glob pattern"),
          path: tool.schema.string().optional().describe("Optional base path"),
        },
        async execute(args, context) {
          await context.ask({
            permission: "glob",
            patterns: [args.pattern],
            always: [args.pattern],
            metadata: { pattern: args.pattern },
          })

          const useVwt = await isChildSession(context.sessionID)
          if (!useVwt) {
            const baseDir = context.worktree
            const globber = new Bun.Glob(args.pattern)
            const matches: string[] = []
            for await (const p of globber.scan({ cwd: baseDir, onlyFiles: true })) {
              matches.push(toPosixPath(p))
            }

            let filtered = matches
            if (args.path) {
              const { rel } = resolveWorktreePath(args.path, context.directory, context.worktree, { allowRoot: true })
              if (rel !== ".") {
                const prefix = rel.endsWith("/") ? rel : rel + "/"
                filtered = matches.filter((m) => m === rel || m.startsWith(prefix))
              }
            }
            filtered.sort((a, b) => a.localeCompare(b))
            return filtered.join("\n") + (filtered.length ? "\n" : "")
          }

          const ws = wsForSession(context.sessionID)
          const info = await vwtInfo(ws, vwtAuthor(context.agent), context.worktree)
          const all = await $.cwd(context.worktree)`git ls-tree -r --name-only ${info.head}`.text()
          let files = all
            .split("\n")
            .map((s) => s.trim())
            .filter(Boolean)

          if (args.path) {
            const { rel } = resolveWorktreePath(args.path, context.directory, context.worktree, { allowRoot: true })
            if (rel !== ".") {
              const prefix = rel.endsWith("/") ? rel : rel + "/"
              files = files.filter((f) => f === rel || f.startsWith(prefix))
            }
          }

          const patterns = $.braces(args.pattern)
          const regexes = patterns.map((p) => {
            const src = stripLeadingDotSlash(p.trim()).replace(/^\//, "")
            // Minimal glob -> regex: **, *, ?, and escaping.
            let re = "^"
            for (let i = 0; i < src.length; i++) {
              const ch = src[i]
              const next = src[i + 1]
              if (ch === "*" && next === "*") {
                const after = src[i + 2]
                if (after === "/") {
                  // "**/" matches zero or more directories.
                  re += "(?:.*/)?"
                  i += 2
                  continue
                }
                re += ".*"
                i++
                continue
              }
              if (ch === "*") {
                re += "[^/]*"
                continue
              }
              if (ch === "?") {
                re += "[^/]"
                continue
              }
              if ("\\.^$+()[]{}|".includes(ch)) {
                re += "\\" + ch
                continue
              }
              re += ch
            }
            re += "$"
            return new RegExp(re)
          })

          const matches = files.filter((f) => regexes.some((r) => r.test(f))).sort((a, b) => a.localeCompare(b))
          return matches.join("\n") + (matches.length ? "\n" : "")
        },
      }),

      vwt_patch: tool({
        description: "Show the git-vwt patch for a session workspace.",
        args: {
          sessionID: tool.schema.string().optional().describe("Session ID (defaults to current)") ,
        },
        async execute(args, context) {
          const sid = args.sessionID ?? context.sessionID
          const ws = wsForSession(sid)
          await context.ask({
            permission: "read",
            patterns: [ws],
            always: [ws],
            metadata: { workspace: ws },
          })
          return await vwtPatch(ws, vwtAuthor(context.agent), context.worktree)
        },
      }),

      vwt_apply: tool({
        description: "Apply a git-vwt workspace patch to the working directory (primary-only).",
        args: {
          sessionID: tool.schema.string().optional().describe("Session ID whose workspace should be applied (defaults to current)") ,
        },
        async execute(args, context) {
          if (await isChildSession(context.sessionID)) {
            throw new Error("subagents can't apply")
          }

          const sid = args.sessionID ?? context.sessionID
          const ws = wsForSession(sid)
          const author = vwtAuthor(context.agent)

          const patch = await vwtPatch(ws, author, context.worktree)
          if (!patch.trim()) {
            return `workspace ${ws} patch is empty\n`
          }

          const files = new Set<string>()
          for (const line of patch.split("\n")) {
            const m = /^diff --git a\/(.+) b\/(.+)$/.exec(line)
            if (!m) continue
            const aPath = m[1].trim()
            const bPath = m[2].trim()
            if (aPath && aPath !== "/dev/null" && aPath !== ".git" && !aPath.startsWith(".git/")) files.add(aPath)
            if (bPath && bPath !== "/dev/null" && bPath !== ".git" && !bPath.startsWith(".git/")) files.add(bPath)
          }
          const changedFiles = Array.from(files).sort((a, b) => a.localeCompare(b))

          await context.ask({
            permission: "edit",
            patterns: changedFiles.length ? changedFiles : ["*"],
            always: ["*"],
            metadata: {
              workspace: ws,
              files: changedFiles.join(", "),
            },
          })

          const res = await vwtApply(ws, author, context.worktree)
          if (res.exitCode === 0) {
            return `applied workspace ${ws} to working directory\n`
          }

          // `git apply --3way` returns exit code 1 when it applies with conflicts.
          const combined = `${res.stdout}\n${res.stderr}`
          if (res.exitCode === 1 && combined.includes("with conflicts")) {
            const out = combined.trim()
            return `applied workspace ${ws} with conflicts\n${out ? out + "\n" : ""}Resolve conflict markers (<<<<<<< >>>>>>>) in the files above.\n`
          }

          const errText = (res.stderr || res.stdout).trim()
          throw new Error(errText || `failed to apply workspace ${ws} (exit ${res.exitCode})`)
        },
      }),
    },
  }
}
