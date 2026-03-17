package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	updateCheckEnvDisable = "VWT_NO_UPDATE_CHECK"
	updateCheckTimeout    = 1500 * time.Millisecond
	updateCheckInterval   = 24 * time.Hour
	latestReleaseAPIURL   = "https://api.github.com/repos/Mansehej/git-vwt/releases/latest"
)

type releaseInfo struct {
	Version string
	URL     string
}

type updateCheckCache struct {
	CheckedAt     time.Time `json:"checked_at"`
	LatestVersion string    `json:"latest_version,omitempty"`
	URL           string    `json:"url,omitempty"`
}

type versionCheckStatus struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version,omitempty"`
	ReleaseURL      string `json:"release_url,omitempty"`
	UpdateAvailable bool   `json:"update_available"`
	CheckEnabled    bool   `json:"check_enabled"`
	CheckPerformed  bool   `json:"check_performed"`
}

type updateChecker struct {
	currentVersion string
	now            func() time.Time
	cacheDir       func() (string, error)
	fetchLatest    func(context.Context) (releaseInfo, error)
	interval       time.Duration
}

var newUpdateChecker = defaultUpdateChecker

func defaultUpdateChecker(currentVersion string) updateChecker {
	return updateChecker{
		currentVersion: currentVersion,
		now:            time.Now,
		cacheDir:       os.UserCacheDir,
		fetchLatest:    fetchLatestRelease,
		interval:       updateCheckInterval,
	}
}

func truthyEnv(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func updateChecksDisabled() bool {
	return truthyEnv(os.Getenv(updateCheckEnvDisable))
}

func canCheckForUpdates(currentVersion string) bool {
	v := strings.TrimSpace(currentVersion)
	return v != "" && v != "dev"
}

func (c updateChecker) message(ctx context.Context, force bool) (string, error) {
	status, err := c.status(ctx, force)
	if err != nil {
		return "", err
	}
	if !status.UpdateAvailable {
		return "", nil
	}
	return fmt.Sprintf("update available: %s -> %s (%s)", c.currentVersion, status.LatestVersion, status.ReleaseURL), nil
}

func (c updateChecker) status(ctx context.Context, force bool) (versionCheckStatus, error) {
	status := versionCheckStatus{
		CurrentVersion: c.currentVersion,
		CheckEnabled:   !updateChecksDisabled() && canCheckForUpdates(c.currentVersion),
	}
	if !status.CheckEnabled {
		return status, nil
	}
	info, err := c.latest(ctx, force)
	if err != nil {
		return versionCheckStatus{}, err
	}
	status.CheckPerformed = true
	status.LatestVersion = info.Version
	status.ReleaseURL = info.URL
	status.UpdateAvailable = isNewerVersion(c.currentVersion, info.Version)
	return status, nil
}

func (c updateChecker) latest(ctx context.Context, force bool) (releaseInfo, error) {
	cachePath, err := c.cachePath()
	if err != nil {
		return releaseInfo{}, err
	}

	cache, cacheOK, _ := readUpdateCheckCache(cachePath)
	if !force && cacheOK && c.now().Sub(cache.CheckedAt) < c.interval && cache.LatestVersion != "" {
		return releaseInfo{Version: cache.LatestVersion, URL: cache.URL}, nil
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, updateCheckTimeout)
	defer cancel()

	info, err := c.fetchLatest(timeoutCtx)
	if err != nil {
		if cacheOK && cache.LatestVersion != "" {
			return releaseInfo{Version: cache.LatestVersion, URL: cache.URL}, nil
		}
		return releaseInfo{}, err
	}

	_ = writeUpdateCheckCache(cachePath, updateCheckCache{
		CheckedAt:     c.now().UTC(),
		LatestVersion: info.Version,
		URL:           info.URL,
	})
	return info, nil
}

func (c updateChecker) cachePath() (string, error) {
	root, err := c.cacheDir()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("empty cache dir")
	}
	return filepath.Join(root, "git-vwt", "update-check.json"), nil
}

func readUpdateCheckCache(path string) (updateCheckCache, bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return updateCheckCache{}, false, nil
		}
		return updateCheckCache{}, false, err
	}
	var cache updateCheckCache
	if err := json.Unmarshal(b, &cache); err != nil {
		return updateCheckCache{}, false, err
	}
	return cache, true, nil
}

func writeUpdateCheckCache(path string, cache updateCheckCache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func fetchLatestRelease(ctx context.Context) (releaseInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseAPIURL, nil)
	if err != nil {
		return releaseInfo{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "git-vwt/"+version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return releaseInfo{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return releaseInfo{}, fmt.Errorf("unexpected release status: %s", resp.Status)
	}

	var payload struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return releaseInfo{}, err
	}
	if strings.TrimSpace(payload.TagName) == "" {
		return releaseInfo{}, fmt.Errorf("latest release missing tag_name")
	}
	if strings.TrimSpace(payload.HTMLURL) == "" {
		payload.HTMLURL = "https://github.com/Mansehej/git-vwt/releases"
	}
	return releaseInfo{Version: payload.TagName, URL: payload.HTMLURL}, nil
}

func isNewerVersion(currentVersion, latestVersion string) bool {
	current, okCurrent := parseVersion(currentVersion)
	latest, okLatest := parseVersion(latestVersion)
	if !okCurrent || !okLatest {
		return false
	}
	maxLen := len(current)
	if len(latest) > maxLen {
		maxLen = len(latest)
	}
	for i := 0; i < maxLen; i++ {
		cur := 0
		lat := 0
		if i < len(current) {
			cur = current[i]
		}
		if i < len(latest) {
			lat = latest[i]
		}
		if lat > cur {
			return true
		}
		if lat < cur {
			return false
		}
	}
	return false
}

func parseVersion(raw string) ([]int, bool) {
	v := strings.TrimSpace(raw)
	v = strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	if idx := strings.IndexAny(v, "+-"); idx >= 0 {
		v = v[:idx]
	}
	if v == "" {
		return nil, false
	}
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			return nil, false
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}
