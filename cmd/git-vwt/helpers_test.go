package main

import (
	"context"
	"os"
	"testing"

	"git-vwt/internal/gitx"
)

func TestShortSHA(t *testing.T) {
	if got := shortSHA(""); got != "" {
		t.Fatalf("got=%q", got)
	}
	if got := shortSHA("abc"); got != "abc" {
		t.Fatalf("got=%q", got)
	}
	if got := shortSHA("0123456789abcdef"); got != "0123456789ab" {
		t.Fatalf("got=%q", got)
	}
}

func TestParseInt64(t *testing.T) {
	if _, err := parseInt64(""); err == nil {
		t.Fatalf("expected error")
	}
	if _, err := parseInt64("  "); err == nil {
		t.Fatalf("expected error")
	}
	if _, err := parseInt64("12x"); err == nil {
		t.Fatalf("expected error")
	}
	if got, err := parseInt64("42"); err != nil || got != 42 {
		t.Fatalf("got=%d err=%v", got, err)
	}
}

func TestResolveTreeAndCommitParentErrors(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")

	gr := gitx.Runner{Dir: dir, Env: os.Environ()}

	if _, err := resolveTree(ctx, gr, "nope"); err == nil {
		t.Fatalf("expected resolveTree error")
	}
	if _, err := commitParent(ctx, gr, "nope"); err == nil {
		t.Fatalf("expected commitParent error")
	}
}

func TestResolveCommitFromIDOrRev_Empty(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	gr := gitx.Runner{Dir: dir, Env: os.Environ()}
	if _, err := resolveCommitFromIDOrRev(ctx, gr, ""); err == nil {
		t.Fatalf("expected error")
	}
}
