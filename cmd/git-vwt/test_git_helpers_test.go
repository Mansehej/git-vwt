package main

import (
	"bytes"
	"os/exec"
	"testing"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, errBuf.String())
	}
	return out.String()
}

func gitExitOK(dir string, args ...string) bool {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	return cmd.Run() == nil
}
