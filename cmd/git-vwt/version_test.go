package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestVersionCommandPrintsVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"version"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
	if code != 0 {
		t.Fatalf("version exit=%d stderr=%s", code, errOut.String())
	}
	if got, want := out.String(), "git-vwt "+version+"\n"; got != want {
		t.Fatalf("unexpected version output: got=%q want=%q", got, want)
	}
}

func TestVersionFlagPrintsVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"--version"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
	if code != 0 {
		t.Fatalf("--version exit=%d stderr=%s", code, errOut.String())
	}
	if got, want := out.String(), "git-vwt "+version+"\n"; got != want {
		t.Fatalf("unexpected --version output: got=%q want=%q", got, want)
	}
}
