#
# Frontend
#
FROM node:24-alpine AS frontend

WORKDIR /app/frontend
RUN corepack enable && corepack prepare pnpm@latest --activate
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN --mount=type=cache,target=/root/.local/share/pnpm/store pnpm install --frozen-lockfile
COPY frontend/ ./
RUN pnpm build

#
# Go build
#
FROM golang:1.26-alpine AS backend

RUN apk add --no-cache gcc musl-dev \
    && apk add --no-cache \
    --repository=https://dl-cdn.alpinelinux.org/alpine/edge/community \
    vips-dev

WORKDIR /app
COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/root/go/pkg/mod go mod download
COPY backend/ ./
RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 go build -ldflags="-s -w" -o /app/voltis .

#
# Runtime
#
FROM alpine:3.23

RUN apk add --no-cache poppler-utils \
    && apk add --no-cache \
    --repository=https://dl-cdn.alpinelinux.org/alpine/edge/community \
    vips

COPY --from=backend /app/voltis /app/voltis
COPY --from=frontend /app/frontend/dist /app/frontend/dist

ENV APP_STATIC_DIR=/app/frontend/dist

LABEL org.opencontainers.image.source=https://github.com/kindlyfire/voltis

EXPOSE 8080
CMD ["/app/voltis", "server"]
