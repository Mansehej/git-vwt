class GitVwt < Formula
  desc "Virtual workspaces for agent-safe editing"
  homepage "https://github.com/Mansehej/git-vwt"
  license "MIT"
  version "0.1.2"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/Mansehej/git-vwt/releases/download/v0.1.2/git-vwt_v0.1.2_darwin_arm64.tar.gz"
      sha256 "5542f573ec7cbc02e521384513d0d00b196dd2e7a231a8d8e76bdd795d31f9d4"
    else
      url "https://github.com/Mansehej/git-vwt/releases/download/v0.1.2/git-vwt_v0.1.2_darwin_amd64.tar.gz"
      sha256 "e8502e5d9218b7c9527cf715202ec91f688e970708c93f01d779bb31592e5f91"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/Mansehej/git-vwt/releases/download/v0.1.2/git-vwt_v0.1.2_linux_arm64.tar.gz"
      sha256 "0ea61a4491d104e21be52237eb402a3e824bb40dbc5e92f602e48cf34693791a"
    else
      url "https://github.com/Mansehej/git-vwt/releases/download/v0.1.2/git-vwt_v0.1.2_linux_amd64.tar.gz"
      sha256 "aa4244cf5ac2017c3e6700c622b0d9904adf912ede3598c81092b4a3afdb039b"
    end
  end

  def install
    bin.install "git-vwt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/git-vwt version")
  end
end
