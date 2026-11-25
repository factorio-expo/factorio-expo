# Built from the repository root (the pnpm workspace): expo-admin with the
# shared packages/expo-ui, whose generated game data must be in place
# (docker compose run expo-game-data).
FROM node:26.13.0-trixie-slim AS base
RUN apt-get update && apt-get upgrade -y && rm -rf /var/lib/apt/lists/*
# Node 25+ no longer bundles corepack.
RUN npm install -g pnpm@12.18.0
WORKDIR /repo
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/expo-web/package.json apps/expo-web/
COPY apps/expo-admin/package.json apps/expo-admin/
COPY packages/expo-ui/package.json packages/expo-ui/
COPY packages/expo-api-types/package.json packages/expo-api-types/
COPY e2e/package.json e2e/

FROM base AS build
RUN pnpm install --frozen-lockfile --filter expo-admin...
COPY packages/expo-ui packages/expo-ui
COPY apps/expo-admin apps/expo-admin
RUN pnpm --filter expo-admin build

FROM base AS deps
RUN pnpm install --prod --frozen-lockfile --filter expo-admin

# Distroless: no shell or package manager; runs as uid 65532.
FROM gcr.io/distroless/nodejs26-debian13:nonroot AS runtime
WORKDIR /app
ENV HOST=0.0.0.0 \
    PORT=4321 \
    NODE_ENV=production
# The workspace layout: the app's node_modules link into the root store.
COPY --from=deps /repo/node_modules ./node_modules
COPY --from=deps /repo/apps/expo-admin/package.json ./apps/expo-admin/
COPY --from=deps /repo/apps/expo-admin/node_modules ./apps/expo-admin/node_modules
COPY --from=build /repo/apps/expo-admin/dist ./apps/expo-admin/dist
EXPOSE 4321
# Node permission model: read-only access to /app (which includes the client
# secret mounted at /app/secrets); no file writes, child processes, workers
# or native addons.
CMD ["--permission", "--allow-fs-read=/app", "--allow-net", "apps/expo-admin/dist/server/entry.mjs"]
