#!/usr/bin/env python3

from __future__ import annotations

import argparse
from pathlib import Path


TARGETS = {
    ("darwin", "arm64"): "git-vwt_{version}_darwin_arm64.tar.gz",
    ("darwin", "amd64"): "git-vwt_{version}_darwin_amd64.tar.gz",
    ("linux", "arm64"): "git-vwt_{version}_linux_arm64.tar.gz",
    ("linux", "amd64"): "git-vwt_{version}_linux_amd64.tar.gz",
}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Update the Homebrew formula for a git-vwt release"
    )
    parser.add_argument("--version", required=True, help="Release version, e.g. v0.1.1")
    parser.add_argument("--checksums", required=True, help="Path to checksums.txt")
    parser.add_argument("--formula", required=True, help="Path to Formula/git-vwt.rb")
    return parser.parse_args()


def load_checksums(path: Path) -> dict[str, str]:
    checksums: dict[str, str] = {}
    for raw_line in path.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line:
            continue
        parts = line.split()
        if len(parts) < 2:
            raise SystemExit(f"invalid checksum line: {raw_line!r}")
        digest, name = parts[0], Path(parts[-1]).name
        checksums[name] = digest
    return checksums


def render_formula(version: str, checksums: dict[str, str]) -> str:
    urls: dict[tuple[str, str], str] = {}
    for key, template in TARGETS.items():
        name = template.format(version=version)
        digest = checksums.get(name)
        if not digest:
            raise SystemExit(f"missing checksum for {name}")
        urls[key] = digest

    return f'''class GitVwt < Formula
  desc "Virtual workspaces for agent-safe editing"
  homepage "https://github.com/Mansehej/git-vwt"
  license "MIT"
  version "{version.removeprefix("v")}"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/Mansehej/git-vwt/releases/download/{version}/git-vwt_{version}_darwin_arm64.tar.gz"
      sha256 "{urls[("darwin", "arm64")]}"
    else
      url "https://github.com/Mansehej/git-vwt/releases/download/{version}/git-vwt_{version}_darwin_amd64.tar.gz"
      sha256 "{urls[("darwin", "amd64")]}"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/Mansehej/git-vwt/releases/download/{version}/git-vwt_{version}_linux_arm64.tar.gz"
      sha256 "{urls[("linux", "arm64")]}"
    else
      url "https://github.com/Mansehej/git-vwt/releases/download/{version}/git-vwt_{version}_linux_amd64.tar.gz"
      sha256 "{urls[("linux", "amd64")]}"
    end
  end

  def install
    bin.install "git-vwt"
  end

  test do
    assert_match version.to_s, shell_output("#{{bin}}/git-vwt version")
  end
end
'''


def main() -> int:
    args = parse_args()
    version = args.version.strip()
    if not version.startswith("v"):
        raise SystemExit("version must start with 'v'")

    formula_path = Path(args.formula)
    checksums = load_checksums(Path(args.checksums))
    formula_path.write_text(render_formula(version, checksums), encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
