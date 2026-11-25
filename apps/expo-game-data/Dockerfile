FROM golang:1.27.4-trixie AS build

WORKDIR /src
COPY go.mod go.sum ./
COPY internal ./internal
COPY apps/expo-game-data ./apps/expo-game-data
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/expo-game-data ./apps/expo-game-data/cmd/expo-game-data

FROM debian:trixie-20261104-slim

RUN apt-get update \
    && apt-get upgrade -y \
    && apt-get install -y --no-install-recommends ca-certificates webp \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/expo-game-data /usr/local/bin/expo-game-data
COPY apps/expo-game-data/factorio-config.ini /app/config.ini
# Compose mounts the maintainer's modsets.json over this copy.
COPY apps/expo-game-data/modsets.json /app/modsets.json
ENTRYPOINT ["/usr/local/bin/expo-game-data"]
