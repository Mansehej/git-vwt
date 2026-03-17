class GitVwt < Formula
  desc "Virtual workspaces for agent-safe editing"
  homepage "https://github.com/Mansehej/git-vwt"
  license "MIT"
  version "0.1.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/Mansehej/git-vwt/releases/download/v0.1.0/git-vwt_v0.1.0_darwin_arm64.tar.gz"
      sha256 "be148b4689d60fd5c8dc5087b0a3fb380358b569aa116593fd8dd30cda354586"
    else
      url "https://github.com/Mansehej/git-vwt/releases/download/v0.1.0/git-vwt_v0.1.0_darwin_amd64.tar.gz"
      sha256 "bcafe06dcd58a48b823f33ec114b35481d117fafb9e3d475b0560c57ab26f27c"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/Mansehej/git-vwt/releases/download/v0.1.0/git-vwt_v0.1.0_linux_arm64.tar.gz"
      sha256 "a4076f122f878a5d0b30b80652edc098d33d8cbc44df898eb55817e9d3b9fcbb"
    else
      url "https://github.com/Mansehej/git-vwt/releases/download/v0.1.0/git-vwt_v0.1.0_linux_amd64.tar.gz"
      sha256 "afa43e5cfd13acc3604d66accfaa35cec2daab7541e6396ae6d5bb7b3b77653f"
    end
  end

  def install
    bin.install "git-vwt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/git-vwt version")
  end
end
