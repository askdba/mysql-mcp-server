# Release checklist

Releases are automated via GitHub Actions and GoReleaser. This doc is the full checklist for maintainers.

## Workflow (humans and coding agents)

Do **not** land user-facing behavior on `main` and walk away. Before or as part of the same work stream:

1. **Issue** — Open a GitHub issue describing the change (or a release-tracking issue with a checklist).
2. **PR** — Prefer a pull request that references the issue. If code already merged to `main` without a PR, open a **retrospective issue** and link commits and the upcoming CHANGELOG section (traceability over branch gymnastics).
3. **CHANGELOG** — Add a versioned section (`[1.7.0-rc.N]` or GA) **before** tagging; keep `[Unreleased]` at the top per [Pre-release](#pre-release).
4. **Tag** — Annotated tag + `git push origin vX.Y.Z` so GoReleaser runs.

Skipping CHANGELOG until after an RC tag ships the wrong story to users and Homebrew; skipping the issue makes releases hard to explain in release notes.

## Release candidates

- Tag format: **`v1.7.0-rc.1`**, **`v1.7.0-rc.2`**, etc. (semver pre-release after the patch segment).
- Update **`CHANGELOG.md`** with an **`[1.7.0-rc.N] - date`** section (see existing **[Unreleased]** / RC headings).
- **`goreleaser`** `release.prerelease: auto` marks GitHub Releases as **pre-release** when the tag indicates an RC.
- RC tags do **not** update the Homebrew tap (`brews.skip_upload: auto`), so `brew upgrade` users stay on the latest GA. Test an RC with the binaries from its GitHub Release (or the Docker image).
- After validation, ship **GA** as **`v1.7.0`** (or merge RC notes into **`v1.7.0 - date`** per section below).

```bash
git tag -a v1.7.0-rc.1 -m "Release candidate v1.7.0-rc.1"
git push origin v1.7.0-rc.1
```

## Pre-release

1. **Update CHANGELOG**
   - Move `[Unreleased]` content under a versioned section, e.g. `## vX.Y.Z - YYYY-MM-DD`.
   - If cutting GA from an RC: change the RC heading to GA (e.g. `[1.6.0.rc1] - date` → `v1.6.0 - date`).
   - Add a new empty `## [Unreleased]` section at the top.
2. **Commit** the CHANGELOG (and any other release-related changes).

## Cut the release

3. **Tag and push** (use the version you put in CHANGELOG):

   ```bash
   git tag vX.Y.Z
   git push origin vX.Y.Z
   ```

   Pushing the tag triggers the **Release** workflow. It will:
   - Run GoReleaser: build binaries, create GitHub Release with changelog, update Homebrew formula (if `HOMEBREW_TAP_TOKEN` is set).
   - Build and push Docker image to `ghcr.io/askdba/mysql-mcp-server`.

   The README version badge reads the latest GitHub release dynamically, so nothing is pushed back to `main`. (`main` requires the **QA Summary** check, which a bot push could not satisfy.)

   Release CHANGELOG commits are pushed straight to `main` by an admin, who bypasses the required check. Don't use `[skip ci]` in those commits: the QA run on `main` is the only CI those commits get.

## Post-release

4. **CHANGELOG on main**  
   If the release was cut from a non-main branch, the default branch (main) may still show an RC or old heading. Update it so the released version is shown:
   - Checkout `main`, pull latest.
   - In `CHANGELOG.md`, replace the RC or pre-release heading with the GA line (e.g. `## v1.6.0 - YYYY-MM-DD`).
   - Commit and push to `main`.

5. **Homebrew tap**
   - **Formula:** GoReleaser updates it for GA tags when `HOMEBREW_TAP_TOKEN` is set (the tap is the only formula source; there is no copy in this repo). Ensure that secret is configured (token with `contents: write` on [askdba/homebrew-tap](https://github.com/askdba/homebrew-tap)).
   - **Tap README:** Not auto-updated. After a release, update [askdba/homebrew-tap](https://github.com/askdba/homebrew-tap) README if needed (version refs, features, install/upgrade instructions), then commit and push to the tap’s `main`.

6. **Local installation** (for you or users):  
   `brew update && brew upgrade mysql-mcp-server`

## Manual formula update (if needed)

If GoReleaser didn’t push the formula or you need to fix it:

```bash
cd /path/to/homebrew-tap
# Edit Formula/mysql-mcp-server.rb (version, URLs, SHAs)
git add Formula/mysql-mcp-server.rb
git commit -m "mysql-mcp-server: update to vX.Y.Z"
git push origin main
```

## Reference

- Release workflow: [.github/workflows/release.yml](../.github/workflows/release.yml)
- GoReleaser config: [.goreleaser.yml](../.goreleaser.yml)
