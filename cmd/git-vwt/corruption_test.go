package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiffExportAndApplyFailWhenBaseTreeObjectIsMissing(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")

	mustWrite(t, filepath.Join(dir, "hello.txt"), "hello\n")
	git(t, dir, "add", "hello.txt")
	git(t, dir, "commit", "-m", "base")
	base := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))
	baseTree := strings.TrimSpace(git(t, dir, "rev-parse", base+"^{tree}"))

	patch := strings.Join([]string{
		"diff --git a/hello.txt b/hello.txt",
		"--- a/hello.txt",
		"+++ b/hello.txt",
		"@@ -1 +1 @@",
		"-hello",
		"+hello world",
		"",
	}, "\n")
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"import", "--base", base, "--id", "p1", "--stdin"}, IO{In: strings.NewReader(patch), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("import exit=%d stderr=%s", code, errOut.String())
			}
		})
	}

	// Corrupt the object database by removing the base tree object.
	objPath := filepath.Join(dir, ".git", "objects", baseTree[:2], baseTree[2:])
	if _, err := os.Stat(objPath); err != nil {
		t.Skipf("tree object not loose at %s: %v", objPath, err)
	}
	if err := os.Remove(objPath); err != nil {
		t.Fatalf("remove %s: %v", objPath, err)
	}

	cases := []struct {
		name string
		args []string
	}{
		{name: "diff", args: []string{"diff", "p1"}},
		{name: "export", args: []string{"export", "p1"}},
		{name: "apply", args: []string{"apply", "p1"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, errOut := bytes.Buffer{}, bytes.Buffer{}
			withChdir(t, dir, func() {
				code := run(ctx, tc.args, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
				if code != 1 {
					t.Fatalf("exit=%d stderr=%s", code, errOut.String())
				}
			})
			if !strings.Contains(errOut.String(), "unable to read tree") {
				t.Fatalf("expected tree error, got: %q", errOut.String())
			}
		})
	}
}
