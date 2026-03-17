package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var targetArchives = map[string]string{
	"darwin_arm64": "git-vwt_%s_darwin_arm64.tar.gz",
	"darwin_amd64": "git-vwt_%s_darwin_amd64.tar.gz",
	"linux_arm64":  "git-vwt_%s_linux_arm64.tar.gz",
	"linux_amd64":  "git-vwt_%s_linux_amd64.tar.gz",
}

func main() {
	var version string
	var checksumsPath string
	var formulaPath string

	flag.StringVar(&version, "version", "", "Release version, e.g. v0.1.1")
	flag.StringVar(&checksumsPath, "checksums", "", "Path to checksums.txt")
	flag.StringVar(&formulaPath, "formula", "", "Path to Formula/git-vwt.rb")
	flag.Parse()

	if version == "" || checksumsPath == "" || formulaPath == "" {
		flag.Usage()
		os.Exit(2)
	}
	if !strings.HasPrefix(version, "v") {
		fmt.Fprintln(os.Stderr, "version must start with 'v'")
		os.Exit(2)
	}

	checksums, err := loadChecksums(checksumsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	contents, err := renderFormula(version, checksums)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := os.WriteFile(formulaPath, []byte(contents), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func loadChecksums(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	checksums := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid checksum line: %q", line)
		}
		checksums[filepath.Base(parts[len(parts)-1])] = parts[0]
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return checksums, nil
}

func renderFormula(version string, checksums map[string]string) (string, error) {
	get := func(key string) (string, error) {
		archive := fmt.Sprintf(targetArchives[key], version)
		digest := checksums[archive]
		if digest == "" {
			return "", fmt.Errorf("missing checksum for %s", archive)
		}
		return digest, nil
	}

	darwinArm64, err := get("darwin_arm64")
	if err != nil {
		return "", err
	}
	darwinAMD64, err := get("darwin_amd64")
	if err != nil {
		return "", err
	}
	linuxArm64, err := get("linux_arm64")
	if err != nil {
		return "", err
	}
	linuxAMD64, err := get("linux_amd64")
	if err != nil {
		return "", err
	}

	trimmedVersion := strings.TrimPrefix(version, "v")
	return fmt.Sprintf(`class GitVwt < Formula
  desc "Virtual workspaces for agent-safe editing"
  homepage "https://github.com/Mansehej/git-vwt"
  license "MIT"
  version %q

  on_macos do
    if Hardware::CPU.arm?
      url %q
      sha256 %q
    else
      url %q
      sha256 %q
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url %q
      sha256 %q
    else
      url %q
      sha256 %q
    end
  end

  def install
    bin.install "git-vwt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/git-vwt version")
  end
end
`,
		trimmedVersion,
		fmt.Sprintf("https://github.com/Mansehej/git-vwt/releases/download/%s/git-vwt_%s_darwin_arm64.tar.gz", version, version), darwinArm64,
		fmt.Sprintf("https://github.com/Mansehej/git-vwt/releases/download/%s/git-vwt_%s_darwin_amd64.tar.gz", version, version), darwinAMD64,
		fmt.Sprintf("https://github.com/Mansehej/git-vwt/releases/download/%s/git-vwt_%s_linux_arm64.tar.gz", version, version), linuxArm64,
		fmt.Sprintf("https://github.com/Mansehej/git-vwt/releases/download/%s/git-vwt_%s_linux_amd64.tar.gz", version, version), linuxAMD64,
	), nil
}
