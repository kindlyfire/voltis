[positional-arguments]
app *args='':
    uv run voltis "$@"

frontend:
    cd frontend && pnpm dev

# Format imports (I), then format code
fmt:
    uv run ruff check --select I --fix && uv run ruff format

check:
    uv run ruff format --check && uv run ruff check && uv run pyright

[positional-arguments]
test *args='':
    uv run pytest "$@"

docker-push-release:
    #!/usr/bin/env bash
    set -euo pipefail
    version=$(grep '^version' pyproject.toml | sed 's/.*"\(.*\)"/\1/')
    image="ghcr.io/kindlyfire/voltis:$version"
    echo "Image: $image"
    if docker manifest inspect "$image" > /dev/null 2>&1; then
        echo "Image $image already exists, skipping push"
        exit 0
    fi
    docker build -t "$image" .
    docker push "$image"