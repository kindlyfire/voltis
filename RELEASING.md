# Releasing

Server and Android releases have independent versions, tags and changelogs.

| Kind    | Command                          | Tag                  | Changelog              |
| ------- | -------------------------------- | -------------------- | ---------------------- |
| Server  | `just release server <version>`  | `v<version>`         | `CHANGELOG.md`         |
| Android | `just release android <version>` | `android-v<version>` | `android/CHANGELOG.md` |

Before running:

- Entries are under `## [Unreleased]` in the changelog.
- On `main`, clean tree, pushed (`main` equals `origin/main`).
- `GITEA_TOKEN=...` in `scripts/.env` (gitignored) or exported. A Gitea token with repository
  write scope.
- `gh` logged in. Server: `docker login ghcr.io`. Android: `android/keystore.properties`.

## What it does

`scripts/release.sh`:

1. Checks the above, and that the tag doesn't exist yet.
2. Moves `[Unreleased]` to `[<version>] - <date>` in the changelog, and bumps versions:
    - Server: the image tag in `docs/installation.md` and `docs/public/compose.yml`.
    - Android: `versionName` and `versionCode` (+1) in `android/app/build.gradle.kts`, and the
      APK link in `docs/mobile.md`.
3. Builds:
    - Server: builds and pushes `ghcr.io/kindlyfire/voltis:<version>`.
    - Android: `./gradlew assembleRelease`, copied to `voltis-<version>.apk`.
4. Commits `Publish v<version>` / `Publish Android v<version>`, tags it, and pushes `main` and the
   tag to Gitea and to GitHub directly (not through the push mirror).
5. Server, stable versions only: points `:latest` at the new image.
6. Creates the release on Gitea and GitHub, with the changelog section as notes. Android releases
   get the APK attached and never become "Latest" on GitHub. Versions with a `-` (`1.2.0-beta.1`)
   are prereleases.

If it fails partway, fix the cause and rerun the same command. When the tag already points at
`HEAD`, it skips the edits and commit and only does what's missing.

The docs site isn't deployed by the script.
