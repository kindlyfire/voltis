#!/usr/bin/env bash
# Publishes a server or Android release: bumps versions and the changelog, builds the Docker image
# or the signed APK, commits and tags on main, pushes to Gitea and GitHub, then creates the
# release on both.
#
# Usage: scripts/release.sh server|android <version>
#
# Rerunning with the same version after a failure resumes: when the tag already points at HEAD,
# the edits and commit are skipped and only the missing pushes, uploads and releases are done.
#
# Needs git, curl, jq, perl, a logged-in `gh`, GITEA_TOKEN (repository write scope), and for
# server releases docker with `docker login ghcr.io`. GIT_REMOTE, GITEA_REPO, GITHUB_REPO and
# IMAGE can be overridden to test against throwaway repositories; GIT_REMOTE must point at
# GITEA_REPO. GitHub gets the push directly rather than waiting for the Gitea push mirror.
set -euo pipefail

GITEA_URL=${GITEA_URL:-https://git.tijlvdb.me}
GITEA_REPO=${GITEA_REPO:-tijlvdb/voltis}
GITHUB_REPO=${GITHUB_REPO:-kindlyfire/voltis}
GIT_REMOTE=${GIT_REMOTE:-origin}
GITHUB_GIT_URL=${GITHUB_GIT_URL:-git@github.com:$GITHUB_REPO.git}
IMAGE=${IMAGE:-ghcr.io/kindlyfire/voltis}

die() { echo "error: $*" >&2; exit 1; }
step() { echo; echo "==> $*"; }

[[ $# -eq 2 ]] || die "usage: $0 server|android <version>"
kind=$1
version=${2#v}
[[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]] || die "invalid version: $version"
prerelease=false
[[ $version == *-* ]] && prerelease=true

case $kind in
    server)
        tag=v$version
        title="v$version"
        changelog=CHANGELOG.md
        tools=(git curl jq perl gh docker)
        ;;
    android)
        tag=android-v$version
        title="Android v$version"
        changelog=android/CHANGELOG.md
        tools=(git curl jq perl gh)
        apk_name=voltis-$version.apk
        apk=android/app/build/outputs/apk/release/$apk_name
        ;;
    *) die "unknown release kind: $kind" ;;
esac

cd "$(git rev-parse --show-toplevel)"

# Changelog section of $version, without its heading and surrounding blank lines
changelog_notes() {
    awk -v h="## [$version]" '
        index($0, h) == 1 { on = 1; next }
        on && /^## \[/ { exit }
        on { print }
    ' "$changelog" | sed -e '/./,$!d' | sed -e ':a' -e '/^\n*$/{$d;N;ba' -e '}'
}

gitea() {
    curl -fsS -H "Authorization: token $GITEA_TOKEN" "$@"
}

# Builds and pushes the versioned image unless the registry already has it
push_image() {
    if docker manifest inspect "$IMAGE:$version" > /dev/null 2>&1; then
        echo "$IMAGE:$version already exists"
        return
    fi
    docker build --build-arg APP_VERSION="$version" -t "$IMAGE:$version" .
    docker push "$IMAGE:$version"
}

build_apk() {
    (cd android && ./gradlew --no-daemon clean assembleRelease)
    cp android/app/build/outputs/apk/release/app-release.apk "$apk"
}

step "Checking"
for t in "${tools[@]}"; do
    command -v "$t" > /dev/null || die "$t is not installed"
done
[[ -n ${GITEA_TOKEN:-} ]] || die "GITEA_TOKEN is not set"
gh auth status -h github.com > /dev/null 2>&1 || die "gh is not logged in (gh auth login)"
remote_url=$(git remote get-url "$GIT_REMOTE")
[[ ${remote_url%.git} == */$GITEA_REPO ]] || die "remote $GIT_REMOTE ($remote_url) is not $GITEA_REPO"
[[ $(git branch --show-current) == main ]] || die "not on main"
[[ -z $(git status --porcelain) ]] || die "working tree is not clean"
if [[ $kind == android ]]; then
    [[ -f android/keystore.properties ]] || die "android/keystore.properties is missing, the APK would be unsigned"
fi
git fetch --quiet "$GIT_REMOTE" main --tags
remote_main=$(git rev-parse "$GIT_REMOTE/main")

resume=false
if git rev-parse -q --verify "refs/tags/$tag" > /dev/null; then
    [[ $(git rev-parse "$tag^{commit}") == $(git rev-parse HEAD) ]] ||
        die "tag $tag exists and is not HEAD"
    git merge-base --is-ancestor "$remote_main" HEAD || die "$GIT_REMOTE/main is ahead of $tag"
    resume=true
    echo "$tag is HEAD, resuming"
else
    [[ $(git rev-parse HEAD) == "$remote_main" ]] || die "main is not in sync with $GIT_REMOTE/main"
    unreleased=$(awk '/^## \[Unreleased\][[:space:]]*$/ { on = 1; next } on && /^## \[/ { exit } on && NF { print }' "$changelog")
    [[ -n $unreleased ]] || die "$changelog has no entries under [Unreleased]"
fi

if [[ $resume == false ]]; then
    step "Updating versions and $changelog"
    sed -i -E "s/^## \[Unreleased\][[:space:]]*$/## [Unreleased]\n\n## [$version] - $(date +%F)/" "$changelog"
    [[ -n $(changelog_notes) ]] || die "could not move [Unreleased] to [$version] in $changelog"
    if [[ $kind == server ]]; then
        for f in docs/installation.md docs/public/compose.yml; do
            sed -i -E "s#ghcr\.io/kindlyfire/[A-Za-z0-9_-]+:[^[:space:]\"']+#$IMAGE:$version#" "$f"
            grep -qF "$IMAGE:$version" "$f" || die "could not update the image in $f"
        done
    else
        gradle=android/app/build.gradle.kts
        code=$(sed -nE 's/^\s*versionCode = ([0-9]+)$/\1/p' "$gradle")
        [[ -n $code ]] || die "versionCode not found in $gradle"
        sed -i -E "s/^(\s*versionCode = )[0-9]+$/\1$((code + 1))/; s/^(\s*versionName = )\"[^\"]*\"$/\1\"$version\"/" "$gradle"
        grep -qF "versionName = \"$version\"" "$gradle" || die "could not update versionName in $gradle"
        apk_url=https://github.com/$GITHUB_REPO/releases/download/$tag/$apk_name
        perl -0pi -e "s#href=\"[^\"]*\\.apk\"#href=\"$apk_url\"#; s#voltis-[^\\s<\"/]+\\.apk(?=</strong>)#$apk_name#" docs/mobile.md
        grep -qF "href=\"$apk_url\"" docs/mobile.md || die "could not update the APK link in docs/mobile.md"
    fi
    git --no-pager diff --stat
fi

step "Building"
if [[ $kind == server ]]; then
    push_image
elif [[ $resume == false || ! -f $apk ]]; then
    build_apk
fi

if [[ $resume == false ]]; then
    step "Committing and tagging $tag"
    git commit --quiet -am "Publish $title"
    git tag "$tag"
fi
git push --atomic "$GIT_REMOTE" main "$tag"
git push --atomic "$GITHUB_GIT_URL" main "$tag"

if [[ $kind == server && $prerelease == false ]]; then
    step "Tagging $IMAGE:latest"
    docker buildx imagetools create -t "$IMAGE:latest" "$IMAGE:$version"
fi

notes=$(mktemp)
trap 'rm -f "$notes"' EXIT
changelog_notes > "$notes"
[[ -s $notes ]] || die "no notes for $version in $changelog"

step "Creating the Gitea release"
release=$(curl -sS -H "Authorization: token $GITEA_TOKEN" "$GITEA_URL/api/v1/repos/$GITEA_REPO/releases/tags/$tag")
release_id=$(jq -r '.id // empty' <<< "$release")
if [[ -z $release_id ]]; then
    release=$(jq -n --arg tag "$tag" --arg name "$title" --rawfile body "$notes" \
        --argjson pre "$prerelease" '{tag_name: $tag, name: $name, body: $body, prerelease: $pre}' |
        gitea -H 'Content-Type: application/json' -d @- "$GITEA_URL/api/v1/repos/$GITEA_REPO/releases")
    release_id=$(jq -r .id <<< "$release")
fi
if [[ $kind == android ]] && ! jq -e --arg n "$apk_name" '.assets[]? | select(.name == $n)' <<< "$release" > /dev/null; then
    gitea -F "attachment=@$apk" "$GITEA_URL/api/v1/repos/$GITEA_REPO/releases/$release_id/assets?name=$apk_name" > /dev/null
fi
echo "$GITEA_URL/$GITEA_REPO/releases/tag/$tag"

step "Creating the GitHub release"
if ! gh release view "$tag" --repo "$GITHUB_REPO" > /dev/null 2>&1; then
    gh_args=(--repo "$GITHUB_REPO" --verify-tag --title "$title" --notes-file "$notes")
    [[ $prerelease == true ]] && gh_args+=(--prerelease)
    # Keep "Latest" on the server release
    [[ $kind == android ]] && gh_args+=(--latest=false)
    gh release create "$tag" "${gh_args[@]}"
else
    gh release view "$tag" --repo "$GITHUB_REPO" --json url --jq .url
fi
if [[ $kind == android ]] &&
    ! gh release view "$tag" --repo "$GITHUB_REPO" --json assets --jq '.assets[].name' | grep -qxF "$apk_name"; then
    gh release upload "$tag" "$apk" --repo "$GITHUB_REPO"
fi
