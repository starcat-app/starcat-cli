# Releasing Starcat CLI

## Repository setup

1. Create the public repositories `starcat-app/starcat-cli` and `starcat-app/homebrew-starcat-cli`.
2. Push both repositories with `main` as the default branch.
3. Add a fine-grained `HOMEBREW_TAP_TOKEN` Actions secret to `starcat-app/starcat-cli`. It only needs Contents read/write permission for `starcat-app/homebrew-starcat-cli`.
4. Enable GitHub Actions and artifact attestations.

## Development versions

Keep the current development or delivery version in `VERSION`. Advance it for
each completed behavior change: patch for compatible fixes, minor for compatible
features, and major for incompatible public interfaces. Use `-dev.N` for
unpublished iterations and increase N for subsequent deliveries of the same
target version.

`make build` injects `VERSION` into the local binary. The Go source keeps its `dev`
fallback; release builds continue to inject the Git tag. Updating `VERSION` does
not create a tag, publish binaries, or update Homebrew.

## Publish a release

After release authorization, set `VERSION` to the intended release tag, update
`CHANGELOG.md`, verify `make build` and `./bin/starcat version`, commit the release
state, and create the matching semantic-version tag:

```bash
release_version="$(cat VERSION)"
git tag -s "${release_version}" -m "Starcat CLI ${release_version}"
git push origin "${release_version}"
```

The Release workflow verifies the code, creates five archives, generates checksums and provenance attestations, publishes the GitHub Release, and updates `Formula/starcat.rb` in the Homebrew tap.

Do not reuse or replace an existing release tag. Publish a new patch version instead.
