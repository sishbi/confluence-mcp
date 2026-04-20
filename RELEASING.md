# Releasing

Releases are automated via [release-please](https://github.com/googleapis/release-please). The version, changelog, and tag are all driven from PR titles — contributors do not tag manually.

## PR title convention

Every PR title must follow [Conventional Commits](https://www.conventionalcommits.org/) and is enforced by the `Validate PR title` check:

```
<type>(optional-scope): <subject>
```

Allowed types: `feat`, `fix`, `perf`, `refactor`, `docs`, `test`, `build`, `ci`, `chore`, `style`, `revert`. Append `!` (e.g. `feat!:`) or add a `BREAKING CHANGE:` footer for a breaking change.

## How a release is cut

1. Merge PRs to `main` as usual. Each squash-merge commit keeps its Conventional Commits title.
2. On every push to `main`, the `Release Please` workflow updates (or opens) a standing Release PR titled `chore(main): release X.Y.Z`. It accumulates a changelog entry per merged PR and bumps the version.
3. When the release is ready, merge the Release PR. release-please creates the `vX.Y.Z` tag.
4. The tag push triggers the `Release` workflow (GoReleaser), which publishes the GitHub release binaries, pushes the Docker image, and opens a Homebrew cask bump PR.
5. The release workflow immediately enables auto-merge on the cask PR. Once CI and PR Title checks pass, GitHub squash-merges it without human intervention.

## Why the cask PR exists

Homebrew casks pin both the versioned download URL and the `sha256` of each archive. The URL embeds the new tag and the SHAs only exist after GoReleaser builds the artifacts, so `Casks/confluence-mcp.rb` must be regenerated on every release. GoReleaser opens the bump as a PR (rather than committing straight to `main`) because branch protection requires one; auto-merge removes the manual step.

## Version bump rules (pre-1.0)

While under `1.0.0`, the bump is one level smaller than standard semver:

| Commit type              | Bump  |
|--------------------------|-------|
| `fix:` / `feat:`         | patch |
| `feat!:` / breaking      | minor |

No accidental 1.0 cut. Remove `bump-minor-pre-major` / `bump-patch-for-minor-pre-major` from `release-please-config.json` when you are ready for 1.0.

## Forcing a specific version

If a PR contains only non-bumping types (e.g. `docs:`, `chore:`) but you still want to cut a release when it merges, append a `Release-As: X.Y.Z` trailer to the squash-merge commit message. release-please will cut that version regardless of commit types.

## Required secrets

- `RELEASE_PAT` — fine-grained PAT (Contents: R/W, Pull requests: R/W) owned by a repo admin. Needed because `GITHUB_TOKEN`-authored pushes and PRs don't trigger downstream workflows, so the tag push from release-please and the cask PR from GoReleaser would otherwise never fire `release.yml`, CI, or PR Title.
- `DOCKER_USERNAME` / `DOCKER_PASSWORD` — Docker Hub credentials for the multi-arch image push.
