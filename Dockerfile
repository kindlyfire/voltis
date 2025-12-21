FROM node:24-alpine AS frontend

WORKDIR /app/frontend
RUN corepack enable && corepack prepare pnpm@latest --activate
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY frontend/ ./
RUN pnpm build


FROM python:3.14-alpine

WORKDIR /app
COPY --from=ghcr.io/astral-sh/uv:latest /uv /uvx /bin/

COPY pyproject.toml uv.lock ./
RUN uv sync --frozen --no-dev --no-install-project
COPY src/ ./src/
RUN uv sync --frozen --no-dev

COPY --from=frontend /app/frontend/dist /app/frontend/dist

ENV PATH="/app/.venv/bin:$PATH"

EXPOSE 8000
CMD ["uv", "run", "--no-sync", "--offline", "voltis", "run", "--host", "0.0.0.0"]