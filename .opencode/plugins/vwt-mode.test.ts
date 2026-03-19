import { describe, expect, test } from "bun:test"

import { __test__ } from "./vwt-mode"

describe("apply_patch semantics", () => {
  test("inserts after change_context instead of appending to EOF", () => {
    const result = __test__.deriveNewContentsFromChunks("a\nc\n", "demo.txt", [
      {
        change_context: "a",
        old_lines: [],
        new_lines: ["b"],
      },
    ])

    expect(result).toBe("a\nb\nc\n")
  })

  test("inserts at EOF when the chunk is marked as end-of-file", () => {
    const result = __test__.deriveNewContentsFromChunks("a\n", "demo.txt", [
      {
        change_context: "a",
        old_lines: [],
        new_lines: ["b"],
        is_end_of_file: true,
      },
    ])

    expect(result).toBe("a\nb\n")
  })

  test("preserves files without a trailing newline during EOF edits", () => {
    const result = __test__.deriveNewContentsFromChunks("a", "demo.txt", [
      {
        old_lines: ["a"],
        new_lines: ["b"],
        is_end_of_file: true,
      },
    ])

    expect(result).toBe("b")
  })

  test("can explicitly add a trailing newline", () => {
    const result = __test__.deriveNewContentsFromChunks("a", "demo.txt", [
      {
        old_lines: ["a"],
        new_lines: ["a", ""],
        is_end_of_file: true,
      },
    ])

    expect(result).toBe("a\n")
  })

  test("can explicitly remove a trailing newline", () => {
    const result = __test__.deriveNewContentsFromChunks("a\n", "demo.txt", [
      {
        old_lines: ["a", ""],
        new_lines: ["a"],
        is_end_of_file: true,
      },
    ])

    expect(result).toBe("a")
  })

  test("parses add-file hunks without forcing a trailing newline", () => {
    const noNewline = __test__.parsePatch(["*** Begin Patch", "*** Add File: demo.txt", "+hello", "*** End Patch"].join("\n"))
    const withNewline = __test__.parsePatch(
      ["*** Begin Patch", "*** Add File: demo.txt", "+hello", "+", "*** End Patch"].join("\n"),
    )

    expect(noNewline.hunks[0]).toEqual({ type: "add", path: "demo.txt", contents: "hello" })
    expect(withNewline.hunks[0]).toEqual({ type: "add", path: "demo.txt", contents: "hello\n" })
  })
})

describe("orphan workspace sweep", () => {
  test("keeps live session workspaces and closes orphaned opencode refs", () => {
    const result = __test__.orphanedOpenCodeWorkspaces(
      ["opencode-live", "opencode-orphan", "other-workspace"],
      [{ id: "live" }, { id: "child/2" }],
    )

    expect(result).toEqual(["opencode-orphan"])
  })
})

describe("update instruction", () => {
  test("renders a user-facing update prompt when a newer release exists", () => {
    const result = __test__.renderUpdateInstruction({
      current_version: "v0.1.0",
      latest_version: "v0.2.0",
      release_url: "https://example.com/release",
      update_available: true,
    })

    expect(result).toContain("git-vwt can be updated from v0.1.0 to v0.2.0")
    expect(result).toContain("ask whether the user wants you to update it")
  })

  test("omits the instruction when no update is available", () => {
    expect(__test__.renderUpdateInstruction({ current_version: "v0.1.0", update_available: false })).toBeUndefined()
  })
})

describe("system prompt", () => {
  test("keeps child-session instructions focused on normal file tools", () => {
    const result = __test__.buildVwtSystemPrompt({
      isChild: true,
      isolatePrimary: false,
    })

    expect(result).toContain("Use normal file tools as usual.")
    expect(result).toContain("Do not try to apply changes to the working directory.")
    expect(result).not.toContain("workspace opencode-")
    expect(result).not.toContain("vwt_apply")
  })

  test("keeps primary-session orchestration guidance without leaking child tool changes", () => {
    const result = __test__.buildVwtSystemPrompt({
      isChild: false,
      isolatePrimary: true,
      updateInstruction: "- Update instruction.",
    })

    expect(result).toContain("VWT mode is enabled")
    expect(result).toContain("Use normal file tools as usual")
    expect(result).toContain("vwt_apply")
    expect(result).toContain("vwt_close")
    expect(result).toContain("- Update instruction.")
  })
})

describe("patch output", () => {
  test("does not mention workspace-specific success text", () => {
    const result = __test__.patchToolSuccessMessage(["M demo.txt"])

    expect(result).toBe("Success. Updated the following files:\nM demo.txt\n")
    expect(result).not.toContain("workspace")
  })
})
