# Build stage
FROM node:22-alpine AS build

WORKDIR /app
RUN corepack enable pnpm
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY . .
RUN pnpm build

# Serve stage
FROM caddy:alpine

COPY --from=build /app/.vitepress/dist /srv
COPY Caddyfile /etc/caddy/Caddyfile

EXPOSE 80
