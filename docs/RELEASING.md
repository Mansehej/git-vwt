# Releasing git-vwt

## Checklist

1. Update `CHANGELOG.md`.
2. Run the verification commands from the repository root:

   ```bash
   go test ./...
   go build -o git-vwt ./cmd/git-vwt
   bun test plugins/vwt-mode.test.ts --cwd .opencode
   ```

3. Commit the release changes.
4. Create an annotated tag:

   ```bash
   git tag -a v0.1.0 -m "v0.1.0"
   ```

5. Push the branch and tag:

   ```bash
   git push origin main
   git push origin v0.1.0
   ```

6. After the GitHub release finishes, update `Formula/git-vwt.rb` to point at the new release assets and checksums, then push that formula change to `main`.

## What the tag does

- Pushing a `v*` tag triggers `.github/workflows/release.yml`.
- The workflow cross-compiles `git-vwt` for Linux, macOS, and Windows.
- Each binary is stamped with the tag via `-X main.version=<tag>`.
- GitHub release notes are generated automatically and categorized using `.github/release.yml`.
- The workflow uploads `.tar.gz` and `.zip` archives plus `checksums.txt` to the GitHub release.
- Homebrew installs those release archives through `Formula/git-vwt.rb`.

## Verification

After the release workflow finishes, verify:

- the GitHub release exists for the tag
- each archive downloads and extracts cleanly
- `git vwt version` prints the tag value from the released binary
- `brew tap Mansehej/git-vwt && brew install git-vwt` installs the expected release on macOS and Linux
