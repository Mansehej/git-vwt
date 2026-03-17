class GitVwt < Formula
  desc "Virtual workspaces for agent-safe editing"
  homepage "https://github.com/Mansehej/git-vwt"
  license "MIT"
  version "0.1.1"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/Mansehej/git-vwt/releases/download/v0.1.1/git-vwt_v0.1.1_darwin_arm64.tar.gz"
      sha256 "d436706939ad2dcab8412bc9c8b73f535740c3407c95d22c44a3113029920f7d"
    else
      url "https://github.com/Mansehej/git-vwt/releases/download/v0.1.1/git-vwt_v0.1.1_darwin_amd64.tar.gz"
      sha256 "804f6e6a302a7fc990e001d3c443bcf32ff0e2fbe825f602f7cec01d0ad7b305"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/Mansehej/git-vwt/releases/download/v0.1.1/git-vwt_v0.1.1_linux_arm64.tar.gz"
      sha256 "c82ad2daa19112bdfd57beed44237b121cdb519a2fe4c1b6b687acdb873de956"
    else
      url "https://github.com/Mansehej/git-vwt/releases/download/v0.1.1/git-vwt_v0.1.1_linux_amd64.tar.gz"
      sha256 "ec9bdb15fea99c4a1ce38913b8f33aaaac7f7127458a1d0bc492caf82c598127"
    end
  end

  def install
    bin.install "git-vwt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/git-vwt version")
  end
end
