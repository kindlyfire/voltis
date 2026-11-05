app *args='':
    # https://github.com/mitranim/gow
    cd backend && gow run . {{ args }}

frontend *args='':
    cd frontend && pnpm dev {{ args }}

dev:
    pnpm dlx concurrently \
        --names backend,frontend \
        --prefix-colors blue,magenta \
        --kill-others \
        "just app server" \
        "just frontend"

fmt:
    cd backend && gofmt -w .

check:
    cd backend && go vet ./...

lint:
    cd backend && golangci-lint run ./...

[positional-arguments]
test *args='':
    cd backend && go test "$@" ./...

# Publish a server or Android release. See scripts/release.sh
release kind version:
    scripts/release.sh {{ kind }} {{ version }}

docker-push-dev:
    #!/usr/bin/env bash
    set -euo pipefail
    image="ghcr.io/kindlyfire/voltis:dev"
    echo "Image: $image"
    docker build --build-arg APP_VERSION=dev -t "$image" .
    docker push "$image"

android *args='assembleDebug':
    cd android && ./gradlew {{ args }}
