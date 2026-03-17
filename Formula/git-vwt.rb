class GitVwt < Formula
  desc "Virtual workspaces for agent-safe editing"
  homepage "https://github.com/Mansehej/git-vwt"
  license "MIT"
  version "0.1.3"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/Mansehej/git-vwt/releases/download/v0.1.3/git-vwt_v0.1.3_darwin_arm64.tar.gz"
      sha256 "6b4698198cac683689880dcc1051fc157224c9922d53372929c38e42ebe9a80a"
    else
      url "https://github.com/Mansehej/git-vwt/releases/download/v0.1.3/git-vwt_v0.1.3_darwin_amd64.tar.gz"
      sha256 "3132ce851b4bdaaa6030f4689c38c6b7b418662b181a5ed8501e15c57edd30ec"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/Mansehej/git-vwt/releases/download/v0.1.3/git-vwt_v0.1.3_linux_arm64.tar.gz"
      sha256 "e13dbc3a84bb64d3120bbadc0add92a6a167e73f9e0d269d16f41580a4543bf6"
    else
      url "https://github.com/Mansehej/git-vwt/releases/download/v0.1.3/git-vwt_v0.1.3_linux_amd64.tar.gz"
      sha256 "054e95daf1b5113170ce69bf43e235876c81fde5c721b11653dad8c92081283b"
    end
  end

  def install
    bin.install "git-vwt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/git-vwt version")
  end
end
