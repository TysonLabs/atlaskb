# AtlasKB Private Homebrew Tap (macOS)

This guide sets up AtlasKB so you can install it via your own private Homebrew tap.

## 1. Create a private tap repository

Create a private repo named `homebrew-atlaskb` (or similar) under your GitHub account/org.

The tap repo should contain:

```
Formula/
  atlaskb.rb
```

## 2. Create and push a release tag in this repo

```bash
git checkout main
git pull
git tag -a v0.1.0 -m "Release v0.1.0"
git push origin v0.1.0
```

## 3. Generate formula into your tap repo

From this repo:

```bash
chmod +x scripts/generate-homebrew-formula.sh
scripts/generate-homebrew-formula.sh \
  --tag v0.1.0 \
  --source-repo https://github.com/<owner>/atlaskb.git \
  --output ../homebrew-atlaskb/Formula/atlaskb.rb
```

Then commit and push in your tap repo:

```bash
cd ../homebrew-atlaskb
git add Formula/atlaskb.rb
git commit -m "atlaskb v0.1.0"
git push
```

## 4. Install from the private tap

```bash
brew tap <owner>/atlaskb https://github.com/<owner>/homebrew-atlaskb.git
brew install <owner>/atlaskb/atlaskb
atlaskb version
```

The formula builds AtlasKB from source at install time, pinned to the tag + commit revision.

### Run AtlasKB as a background service

```bash
atlaskb setup
brew services start atlaskb
brew services list | grep atlaskb
```

Stop it with:

```bash
brew services stop atlaskb
```

## 5. Publish updates

For each new release:

1. Tag and push a new version in this repo (`vX.Y.Z`).
2. Re-run `scripts/generate-homebrew-formula.sh --tag vX.Y.Z ...`.
3. Commit/push updated `Formula/atlaskb.rb` in your tap repo.
4. Users run `brew upgrade atlaskb`.

## 6. Automate formula publishing with GitHub Actions

This repo includes [release-homebrew-tap.yml](../.github/workflows/release-homebrew-tap.yml), which runs on `v*` tag pushes and updates your tap automatically.

Configure these repository settings in `tgeorge06/atlaskb`:

- Required secret: `HOMEBREW_TAP_TOKEN`
  - Personal access token with `repo` scope that can push to `tgeorge06/homebrew-atlaskb`.
- Optional variable: `HOMEBREW_TAP_REPO`
  - Default is `<owner>/homebrew-atlaskb`.
- Optional variable: `HOMEBREW_SOURCE_REPO`
  - Default is `https://github.com/<owner>/<repo>.git`.

Once configured, a new tag is enough:

```bash
git tag -a v0.2.0 -m "Release v0.2.0"
git push origin v0.2.0
```

The workflow will regenerate `Formula/atlaskb.rb` and push it to the tap repo `main` branch.

## Notes

- The generated formula defaults to the current repo's `origin` remote if `--source-repo` is not provided.
- For private source repos, use `https://github.com/...` URLs and ensure your Git credentials are configured for private repo access.
- `atlaskb version` is build-injected through ldflags; Homebrew test validates the installed version.
