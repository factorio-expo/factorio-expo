# cwebp, libwebp's encoder, as one static binary (no libpng/libjpeg: the API
# pipes it a PPM), so it runs on distroless next to the API. Built from the
# release tarball pinned by checksum; bump both together.
FROM debian:trixie-20261020-slim AS cwebp
ARG LIBWEBP_VERSION=1.6.0
ARG LIBWEBP_SHA256=e4ab7009bf0629fd11982d4c2aa83964cf244cffba7347ecd39019a9e38c4564
RUN apt-get update && \
    apt-get upgrade -y && \
    apt-get install -y --no-install-recommends build-essential cmake ca-certificates curl && \
    rm -rf /var/lib/apt/lists/*
WORKDIR /src
RUN curl -fsSL -o libwebp.tar.gz "https://storage.googleapis.com/downloads.webmproject.org/releases/webp/libwebp-${LIBWEBP_VERSION}.tar.gz" && \
    echo "${LIBWEBP_SHA256}  libwebp.tar.gz" | sha256sum -c - && \
    tar xzf libwebp.tar.gz --strip-components=1
RUN cmake -S . -B build  \
    -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF -DCMAKE_EXE_LINKER_FLAGS=-static  \
    -DWEBP_LINK_STATIC=ON ILD_CWEBP=ON -DWEBP_BUILD_DWEBP=OFF  \
    -DWEBP_BUILD_GIF2WEBP=OFF ILD_IMG2WEBP=OFF -DWEBP_BUILD_VWEBP=OFF \
    -DWEBP_BUILD_WEBPINFO=OFF -DWEBP_BUILD_WEBPMUX=OFF -DWEBP_BUILD_ANIM_UTILS=OFF -DWEBP_BUILD_EXTRAS=OFF &&  \
    cmake --build build -j"$(nproc)" --target cwebp &&  \
    install -D build/cwebp /out/cwebp &&  \
    ! ldd /out/cwebp &&  \
    /out/cwebp -version

FROM golang:1.27.4-trixie AS build

WORKDIR /src
COPY go.mod go.sum ./
COPY internal ./internal
COPY apps/expo-api ./apps/expo-api
# The hand-written community names, embedded for the search synonyms.
COPY apps/expo-game-data/communitynames ./apps/expo-game-data/communitynames
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/expo-api ./apps/expo-api/cmd/expo-api

# Static binaries: distroless carries only CA certs, tzdata and /etc/passwd.
# The API runs as the unprivileged nonroot user (65532); the only files it
# writes are in its storage mount (API_STORAGE_DIR), which expo-storage-init
# hands to that user.
FROM gcr.io/distroless/static-debian13:nonroot

COPY --from=build /out/expo-api /usr/local/bin/expo-api
COPY --from=cwebp /out/cwebp /usr/local/bin/cwebp
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/expo-api"]
