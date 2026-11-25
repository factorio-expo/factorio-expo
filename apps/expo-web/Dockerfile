# Built from the repository root (the pnpm workspace): expo-web with the
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
ARG PUBLIC_GAME_ICONS_BASE_URL=/game-icons/
ENV PUBLIC_GAME_ICONS_BASE_URL=${PUBLIC_GAME_ICONS_BASE_URL}
ARG PUBLIC_SITE_URL=http://localhost:3000
ENV PUBLIC_SITE_URL=${PUBLIC_SITE_URL}
ARG PUBLIC_API_BASE_URL=http://localhost:8080
ENV PUBLIC_API_BASE_URL=${PUBLIC_API_BASE_URL}
# Empty: no Turnstile on the sign-up forms.
ARG PUBLIC_TURNSTILE_SITE_KEY=
ENV PUBLIC_TURNSTILE_SITE_KEY=${PUBLIC_TURNSTILE_SITE_KEY}
# Who runs the site, as JSON (src/lib/legal.ts); empty shows placeholders.
ARG PUBLIC_LEGAL=
ENV PUBLIC_LEGAL=${PUBLIC_LEGAL}
RUN pnpm install --frozen-lockfile --filter expo-web...
COPY packages/expo-ui packages/expo-ui
COPY packages/expo-api-types packages/expo-api-types
# The API's OpenAPI document: scripts/api-docs.ts builds the read API docs from it.
COPY apps/expo-api/openapi apps/expo-api/openapi
COPY apps/expo-web apps/expo-web
RUN pnpm --filter expo-web build

# The standalone server imports its runtime dependencies (marked,
# sanitize-html, ...) from node_modules, so ship the production ones.
FROM base AS deps
RUN pnpm install --prod --frozen-lockfile --filter expo-web

# Distroless: no shell or package manager; runs as uid 65532.
FROM gcr.io/distroless/nodejs26-debian13:nonroot AS runtime
WORKDIR /app
ENV HOST=0.0.0.0 \
    PORT=4321 \
    NODE_ENV=production
# The workspace layout: the app's node_modules link into the root store.
COPY --from=deps /repo/node_modules ./node_modules
COPY --from=deps /repo/apps/expo-web/package.json ./apps/expo-web/
COPY --from=deps /repo/apps/expo-web/node_modules ./apps/expo-web/node_modules
COPY --from=build /repo/apps/expo-web/dist ./apps/expo-web/dist
EXPOSE 4321
# Node permission model: read-only access to /app; no file writes, child
# processes, workers or native addons. --allow-net is all-or-nothing (the
# server listens and calls expo-api).
CMD ["--permission", "--allow-fs-read=/app", "--allow-net", "apps/expo-web/dist/server/entry.mjs"]
