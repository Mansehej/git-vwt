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
