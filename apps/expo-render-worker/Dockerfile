FROM golang:1.27.4-trixie AS build

WORKDIR /src
COPY go.mod go.sum ./
COPY internal ./internal
COPY apps/expo-render-worker ./apps/expo-render-worker
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/expo-render-worker ./apps/expo-render-worker/cmd/expo-render-worker

# Static binary on distroless (no shell or curl): the compose healthcheck runs
# `expo-render-worker healthcheck`. Runs as root because the preview inbox
# volume is shared with the root-run Factorio containers.
FROM gcr.io/distroless/static-debian13

COPY --from=build /out/expo-render-worker /usr/local/bin/expo-render-worker
EXPOSE 8081
ENTRYPOINT ["/usr/local/bin/expo-render-worker"]
