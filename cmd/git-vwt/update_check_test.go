package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		current string
		latest  string
		want    bool
	}{
		{current: "v0.1.0", latest: "v0.1.1", want: true},
		{current: "v0.1.0", latest: "v0.2.0", want: true},
		{current: "v0.1.0", latest: "v0.1.0", want: false},
		{current: "v0.2.0", latest: "v0.1.9", want: false},
		{current: "v1.2.0", latest: "v1.2.0-beta.1", want: false},
	}
	for _, tt := range tests {
		if got := isNewerVersion(tt.current, tt.latest); got != tt.want {
			t.Fatalf("isNewerVersion(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
		}
	}
}

func TestUpdateCheckerUsesFreshCache(t *testing.T) {
	cacheRoot := t.TempDir()
	now := time.Date(2026, 3, 17, 12, 0, 0, 0, time.UTC)
	cachePath := filepath.Join(cacheRoot, "git-vwt", "update-check.json")
	if err := writeUpdateCheckCache(cachePath, updateCheckCache{
		CheckedAt:     now.Add(-time.Hour),
		LatestVersion: "v0.2.0",
		URL:           "https://example.com/release",
	}); err != nil {
		t.Fatal(err)
	}

	fetchCalls := 0
	checker := updateChecker{
		currentVersion: "v0.1.0",
		now:            func() time.Time { return now },
		cacheDir:       func() (string, error) { return cacheRoot, nil },
		fetchLatest: func(ctx context.Context) (releaseInfo, error) {
			fetchCalls++
			return releaseInfo{Version: "v9.9.9", URL: "https://example.com/ignored"}, nil
		},
		interval: 24 * time.Hour,
	}

	msg, err := checker.message(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if fetchCalls != 0 {
		t.Fatalf("expected cached result, fetch called %d times", fetchCalls)
	}
	if want := "update available: v0.1.0 -> v0.2.0 (https://example.com/release)"; msg != want {
		t.Fatalf("unexpected message: got=%q want=%q", msg, want)
	}
}

func TestUpdateCheckerWritesCacheAfterFetch(t *testing.T) {
	cacheRoot := t.TempDir()
	now := time.Date(2026, 3, 17, 12, 0, 0, 0, time.UTC)
	checker := updateChecker{
		currentVersion: "v0.1.0",
		now:            func() time.Time { return now },
		cacheDir:       func() (string, error) { return cacheRoot, nil },
		fetchLatest: func(ctx context.Context) (releaseInfo, error) {
			return releaseInfo{Version: "v0.2.0", URL: "https://example.com/release"}, nil
		},
		interval: 24 * time.Hour,
	}

	msg, err := checker.message(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if want := "update available: v0.1.0 -> v0.2.0 (https://example.com/release)"; msg != want {
		t.Fatalf("unexpected message: got=%q want=%q", msg, want)
	}

	cache, ok, err := readUpdateCheckCache(filepath.Join(cacheRoot, "git-vwt", "update-check.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected cache to be written")
	}
	if cache.LatestVersion != "v0.2.0" || cache.URL != "https://example.com/release" {
		t.Fatalf("unexpected cache contents: %#v", cache)
	}
}

func TestCmdVersionCheckReportsUpdateStatus(t *testing.T) {
	origVersion := version
	origNewUpdateChecker := newUpdateChecker
	t.Cleanup(func() {
		version = origVersion
		newUpdateChecker = origNewUpdateChecker
	})

	version = "v0.1.0"
	newUpdateChecker = func(currentVersion string) updateChecker {
		return updateChecker{
			currentVersion: currentVersion,
			now:            time.Now,
			cacheDir:       func() (string, error) { return t.TempDir(), nil },
			fetchLatest: func(ctx context.Context) (releaseInfo, error) {
				return releaseInfo{Version: "v0.2.0", URL: "https://example.com/release"}, nil
			},
			interval: time.Hour,
		}
	}

	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"version", "--check"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
	if code != 0 {
		t.Fatalf("version --check exit=%d stderr=%s", code, errOut.String())
	}
	if got, want := out.String(), "git-vwt v0.1.0\n"; got != want {
		t.Fatalf("unexpected stdout: got=%q want=%q", got, want)
	}
	if !strings.Contains(errOut.String(), "update available: v0.1.0 -> v0.2.0") {
		t.Fatalf("expected update notice, got=%q", errOut.String())
	}
}

func TestCmdVersionCheckJSONReportsStatus(t *testing.T) {
	origVersion := version
	origNewUpdateChecker := newUpdateChecker
	t.Cleanup(func() {
		version = origVersion
		newUpdateChecker = origNewUpdateChecker
	})

	version = "v0.1.0"
	newUpdateChecker = func(currentVersion string) updateChecker {
		return updateChecker{
			currentVersion: currentVersion,
			now:            time.Now,
			cacheDir:       func() (string, error) { return t.TempDir(), nil },
			fetchLatest: func(ctx context.Context) (releaseInfo, error) {
				return releaseInfo{Version: "v0.2.0", URL: "https://example.com/release"}, nil
			},
			interval: time.Hour,
		}
	}

	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"version", "--check", "--json"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
	if code != 0 {
		t.Fatalf("version --check --json exit=%d stderr=%s", code, errOut.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output, got=%q", errOut.String())
	}
	var status versionCheckStatus
	if err := json.Unmarshal(out.Bytes(), &status); err != nil {
		t.Fatalf("invalid json output: %v; output=%q", err, out.String())
	}
	if status.CurrentVersion != "v0.1.0" || status.LatestVersion != "v0.2.0" || !status.UpdateAvailable || !status.CheckPerformed {
		t.Fatalf("unexpected status: %#v", status)
	}
}

func TestCmdVersionCheckJSONHonorsDisableEnv(t *testing.T) {
	origVersion := version
	t.Cleanup(func() { version = origVersion })
	t.Setenv(updateCheckEnvDisable, "1")
	version = "v0.1.0"

	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"version", "--check", "--json"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
	if code != 0 {
		t.Fatalf("version --check --json exit=%d stderr=%s", code, errOut.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output, got=%q", errOut.String())
	}
	var status versionCheckStatus
	if err := json.Unmarshal(out.Bytes(), &status); err != nil {
		t.Fatalf("invalid json output: %v; output=%q", err, out.String())
	}
	if status.CheckEnabled || status.CheckPerformed || status.UpdateAvailable {
		t.Fatalf("unexpected disabled status: %#v", status)
	}
}
