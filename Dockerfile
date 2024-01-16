FROM node:20-alpine AS base
ENV PNPM_HOME="/pnpm" NUXT_DATA_DIR="/data"
ENV PATH="$PNPM_HOME:$PATH"
RUN corepack enable
RUN mkdir -p /app /data
WORKDIR /app
COPY . .

FROM base AS prod-deps
RUN --mount=type=cache,id=pnpm,target=/pnpm/store pnpm install --prod --frozen-lockfile

FROM base AS build
RUN --mount=type=cache,id=pnpm,target=/pnpm/store pnpm install --frozen-lockfile
RUN NITRO_PRESET=node-cluster pnpm build

FROM base
COPY --from=prod-deps /app/node_modules /app/node_modules
COPY --from=build /app/.output /app/.output
EXPOSE 3000
CMD [ "node", ".output/server/index.mjs" ]