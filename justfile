app *args='':
    cd backend && gow run . {{ args }}

frontend *args='':
    cd frontend && pnpm dev {{ args }}

fmt:
    cd backend && gofmt -w .

check:
    cd backend && go vet ./...

lint:
    cd backend && golangci-lint run ./...

[positional-arguments]
test *args='':
    cd backend && go test "$@" ./...

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

docker-push-dev:
    #!/usr/bin/env bash
    set -euo pipefail
    image="ghcr.io/kindlyfire/voltis:dev"
    echo "Image: $image"
    docker build -t "$image" .
    docker push "$image"
