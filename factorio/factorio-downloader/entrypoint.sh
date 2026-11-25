#!/bin/sh
set -eu

FACTORIO_DIR=/app/factorio
DOWNLOADS_DIR=/app/downloads
FACTORIO_VERSION="${FACTORIO_VERSION:-2.1.24}"
ARCHIVE_PATH="$DOWNLOADS_DIR/factorio-${FACTORIO_VERSION}-linux64.tar.xz"
VERSION_PATH="$FACTORIO_DIR/.version"

mkdir -p "$FACTORIO_DIR" "$DOWNLOADS_DIR"

if [ -f "$FACTORIO_DIR/bin/x64/factorio" ] \
    && [ -f "$VERSION_PATH" ] \
    && [ "$(cat "$VERSION_PATH")" = "$FACTORIO_VERSION" ]; then
    echo "Factorio exists, skipping download"
    exit 0
fi

if [ -z "${FACTORIO_USERNAME:-}" ] || [ -z "${FACTORIO_TOKEN:-}" ]; then
    echo "Could not authenticate, cannot download Factorio; set FACTORIO_USERNAME and FACTORIO_TOKEN"
    exit 1
fi

if [ ! -f "$ARCHIVE_PATH" ]; then
    echo "Downloading Factorio $FACTORIO_VERSION..."
    TEMP_ARCHIVE="$ARCHIVE_PATH.tmp"
    rm -f "$TEMP_ARCHIVE"
    curl --fail --show-error --location \
        "https://www.factorio.com/get-download/${FACTORIO_VERSION}/expansion/linux64?username=$FACTORIO_USERNAME&token=$FACTORIO_TOKEN" \
        --output "$TEMP_ARCHIVE"
    mv "$TEMP_ARCHIVE" "$ARCHIVE_PATH"
fi

echo "Extracting Factorio..."
tar xf "$ARCHIVE_PATH" -C /app
printf '%s\n' "$FACTORIO_VERSION" > "$VERSION_PATH"
