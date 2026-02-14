#
# vmeta build
#
FROM astral/uv:python3.14-alpine AS builder

ARG VMETA_REF=472ce8ca5f95d4250bad4d8183437bc9ca158cf2

WORKDIR /build

RUN apk add --no-cache \
    curl \
    git \
    vips-dev \
    musl-dev \
    gcc \
    g++ \
    patchelf
ENV RUSTUP_HOME=/usr/local/rustup \
    CARGO_HOME=/usr/local/cargo \
    PATH=/usr/local/cargo/bin:$PATH
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain stable
RUN --mount=type=cache,target=/root/.cache/uv uv tool install maturin
RUN git init && \
    git remote add origin https://git.tijlvdb.me/tijlvdb/vmeta.git && \
    git fetch --depth 1 origin "$VMETA_REF" && \
    git checkout FETCH_HEAD
RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/build/target \
    PYO3_USE_ABI3_FORWARD_COMPATIBILITY=1 maturin build --release --features pyo3 --skip-auditwheel && \
    cp target/wheels/vmeta-*.whl /tmp/

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
# Backend (runtime)
#
FROM python:3.14-alpine

RUN apk add --no-cache vips poppler-utils

WORKDIR /app
COPY --from=ghcr.io/astral-sh/uv:latest /uv /uvx /bin/

COPY pyproject.toml uv.lock ./
RUN --mount=type=cache,target=/root/.cache/uv uv sync --frozen --no-dev --no-install-project --no-install-package vmeta
COPY src/ ./src/
RUN --mount=type=cache,target=/root/.cache/uv uv sync --frozen --no-dev --no-install-package vmeta

COPY --from=builder /tmp/vmeta-*.whl /tmp/
RUN --mount=type=cache,target=/root/.cache/uv \
    sh -c 'uv pip install --no-deps /tmp/vmeta-*.whl' && rm /tmp/vmeta-*.whl

COPY --from=frontend /app/frontend/dist /app/frontend/dist

RUN echo -e '#!/bin/sh\nexec uv run --no-sync --offline voltis "$@"' > ./voltis && chmod +x ./voltis

ENV PATH="/app/.venv/bin:$PATH"
LABEL org.opencontainers.image.source=https://github.com/kindlyfire/voltis

EXPOSE 8000
CMD ["uv", "run", "--no-sync", "--offline", "voltis", "run", "--host", "0.0.0.0", "--migrate"]