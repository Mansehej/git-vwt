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

function isVwtAgentName(agent: string | undefined): boolean {
  const a = String(agent ?? "").trim().toLowerCase()
  return a === "vwt" || a.startsWith("vwt-") || a.startsWith("vwt_")
}

function vwtAuthor(agent: string): string {
  const a = String(agent ?? "").trim()
  return a ? `opencode:${a}` : "opencode"
}

export const VwtModePlugin: Plugin = async ({ client, $, worktree: projectWorktree }) => {
  const defaultEnabled = truthyEnv(process.env.OPENCODE_VWT)

  const explicitlyEnabled = new Set<string>()
  const explicitlyDisabled = new Set<string>()
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

  async function isVwtEnabled(sessionID: string, visiting?: Set<string>): Promise<boolean> {
    if (explicitlyDisabled.has(sessionID)) return false
    if (explicitlyEnabled.has(sessionID)) return true
    if (defaultEnabled) {
      explicitlyEnabled.add(sessionID)
      return true
    }

    const seen = visiting ?? new Set<string>()
    if (seen.has(sessionID)) return false
    seen.add(sessionID)

    const parentID = await getParentID(sessionID)
    if (!parentID) return false
    const parentEnabled = await isVwtEnabled(parentID, seen)
    if (parentEnabled) {
      explicitlyEnabled.add(sessionID)
      return true
    }
    return false
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
    return await $.cwd(cwd)`${prefix} --ws ${ws} --agent ${agent} search ${pattern} ${pathspec}`.text()
  }

  async function vwtPatch(ws: string, agent: string, cwd: string): Promise<string> {
    await ensureWsOpen(ws, agent, cwd)
    const prefix = await vwtPrefix(cwd)
    return await $.cwd(cwd)`${prefix} --ws ${ws} --agent ${agent} patch`.text()
  }

  async function vwtApply(ws: string, agent: string, cwd: string): Promise<string> {
    await ensureWsOpen(ws, agent, cwd)
    const prefix = await vwtPrefix(cwd)
    return await $.cwd(cwd)`${prefix} --ws ${ws} --agent ${agent} apply`.text()
  }

  return {
    async event({ event }) {
      if (event.type === "session.created") {
        const info: any = (event as any).properties?.info
        if (info?.id) parentBySession.set(info.id, info.parentID ?? null)
        return
      }
      if (event.type === "session.deleted") {
        const info: any = (event as any).properties?.info
        if (info?.id) {
          parentBySession.delete(info.id)
          explicitlyEnabled.delete(info.id)
          explicitlyDisabled.delete(info.id)
          lastNotifiedPatchHashBySession.delete(info.id)
        }
        return
      }
      if (event.type !== "session.idle") return

      const sessionID: string | undefined = (event as any).properties?.sessionID
      if (!sessionID) return
      if (!(await isVwtEnabled(sessionID))) return
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

    async "chat.message"(input) {
      if (isVwtAgentName(input.agent)) {
        explicitlyDisabled.delete(input.sessionID)
        explicitlyEnabled.add(input.sessionID)
      }
    },

    async "command.execute.before"(input, output) {
      const cmd = String(input.command ?? "").trim().toLowerCase()
      if (cmd === "vwt-on" || cmd === "vwt_on") {
        explicitlyDisabled.delete(input.sessionID)
        explicitlyEnabled.add(input.sessionID)
        const ws = wsForSession(input.sessionID)
        try {
          await ensureWsOpen(ws, "opencode", projectWorktree)
          await client.tui.showToast({
            body: {
              title: "VWT enabled",
              message: `VWT mode enabled for this session. Workspace: ${ws}`,
              variant: "success",
              duration: 5000,
            },
          })
        } catch (err: any) {
          await client.tui.showToast({
            body: {
              title: "VWT enable failed",
              message: String(err?.message ?? err),
              variant: "error",
              duration: 10000,
            },
          })
        }
        output.parts = []
        return
      }

      if (cmd === "vwt-off" || cmd === "vwt_off") {
        explicitlyEnabled.delete(input.sessionID)
        explicitlyDisabled.add(input.sessionID)
        const ws = wsForSession(input.sessionID)
        await client.tui.showToast({
          body: {
            title: "VWT disabled",
            message: `VWT mode disabled for this session. Workspace remains: ${ws}`,
            variant: "info",
            duration: 5000,
          },
        })
        output.parts = []
        return
      }

      if (cmd === "vwt-status" || cmd === "vwt_status") {
        const enabled = await isVwtEnabled(input.sessionID)
        const ws = wsForSession(input.sessionID)
        await client.tui.showToast({
          body: {
            title: "VWT status",
            message: enabled ? `enabled (workspace: ${ws})` : "disabled",
            variant: "info",
            duration: 5000,
          },
        })
        output.parts = []
      }
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

      if (input.tool === "patch" || input.tool === "multiedit") {
        if (await isVwtEnabled(input.sessionID)) {
          throw new Error(`'${input.tool}' is disabled in VWT mode. Use 'edit' or 'write' instead.`)
        }
      }
    },

    async "shell.env"(input, output) {
      if (!input.sessionID) return
      if (!(await isVwtEnabled(input.sessionID))) return
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
          const { abs, rel } = resolveWorktreePath(args.filePath, context.directory, context.worktree)
          await context.ask({
            permission: "read",
            patterns: [rel],
            always: [rel],
            metadata: { path: rel },
          })

          const enabled = await isVwtEnabled(context.sessionID)
          const content = enabled
            ? await vwtRead(wsForSession(context.sessionID), vwtAuthor(context.agent), context.worktree, rel)
            : await fs.readFile(abs, "utf8")

          return formatLineNumbered(content, args.offset, args.limit)
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

          const enabled = await isVwtEnabled(context.sessionID)
          if (enabled) {
            const ws = wsForSession(context.sessionID)
            const out = await vwtWrite(ws, vwtAuthor(context.agent), context.worktree, rel, args.content)
            return `wrote ${rel} (workspace: ${ws})\n${out}`
          }

          await fs.mkdir(path.dirname(abs), { recursive: true })
          await fs.writeFile(abs, args.content, "utf8")
          return `wrote ${rel}\n`
        },
      }),

      edit: tool({
        description: "Edit a file by exact string replacement (VWT-aware when enabled).",
        args: {
          filePath: tool.schema.string().describe("Path to the file"),
          oldText: tool.schema.string().describe("Exact text to replace"),
          newText: tool.schema.string().describe("Replacement text"),
        },
        async execute(args, context) {
          const { abs, rel } = resolveWorktreePath(args.filePath, context.directory, context.worktree)
          await context.ask({
            permission: "edit",
            patterns: [rel],
            always: [rel],
            metadata: { path: rel },
          })

          const enabled = await isVwtEnabled(context.sessionID)
          const ws = wsForSession(context.sessionID)

          const before = enabled
            ? await vwtRead(ws, vwtAuthor(context.agent), context.worktree, rel)
            : await fs.readFile(abs, "utf8")

          const first = before.indexOf(args.oldText)
          if (first < 0) throw new Error("oldText not found")
          const second = before.indexOf(args.oldText, first + args.oldText.length)
          if (second >= 0) throw new Error("oldText is not unique; provide a more specific match")

          const after = before.slice(0, first) + args.newText + before.slice(first + args.oldText.length)
          if (enabled) {
            const out = await vwtWrite(ws, vwtAuthor(context.agent), context.worktree, rel, after)
            return `edited ${rel} (workspace: ${ws})\n${out}`
          }

          await fs.writeFile(abs, after, "utf8")
          return `edited ${rel}\n`
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

          const enabled = await isVwtEnabled(context.sessionID)
          if (enabled) {
            const ws = wsForSession(context.sessionID)
            const pathspec: string[] = []
            if (args.path) {
              const { rel } = resolveWorktreePath(args.path, context.directory, context.worktree, { allowRoot: true })
              if (rel !== ".") pathspec.push(rel)
            }
            if (args.include) pathspec.push(args.include)
            return await vwtSearch(ws, vwtAuthor(context.agent), context.worktree, args.pattern, pathspec)
          }

          const cwd = context.worktree
          const searchPath = args.path
            ? resolveWorktreePath(args.path, context.directory, context.worktree, { allowRoot: true }).abs
            : cwd

          const extra: string[] = []
          if (args.include) extra.push("--glob", args.include)

          const res = await $.cwd(cwd)`rg --no-heading --line-number --color never ${extra} ${args.pattern} ${searchPath}`
            .nothrow()
            .quiet()
          if (res.exitCode === 0) return res.text()
          if (res.exitCode === 1) return ""
          throw new Error(res.stderr.toString())
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

          const enabled = await isVwtEnabled(context.sessionID)
          if (enabled) {
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

          const enabled = await isVwtEnabled(context.sessionID)
          if (enabled) {
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
          }

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
          await context.ask({
            permission: "vwt_apply",
            patterns: [ws],
            always: [ws],
            metadata: { workspace: ws },
          })
          await vwtApply(ws, vwtAuthor(context.agent), context.worktree)
          return `applied workspace ${ws} to working directory\n`
        },
      }),
    },
  }
}
