#!/bin/sh
set -eu

mkdir -p /app/data/mods /app/data/saves

# The mods directory lives in a persistent volume, so the bridge mod baked into
# the image is refreshed on every start. Factorio manages mod-list.json itself
# and enables newly discovered mods by default.
rm -rf /app/data/mods/bp-preview-bridge_*
rm -rf /app/data/mods/bp-preview-bridge
rm -rf /app/data/mods/factorio-expo-bridge_*
rm -rf /app/data/mods/factorio-expo-bridge
cp -r /app/mod/factorio-expo-bridge /app/data/mods/

#export LIBGL_ALWAYS_SOFTWARE=1
FACTORIO_RCON_PORT="${FACTORIO_RCON_PORT:-27015}"
FACTORIO_RCON_PASSWORD="${FACTORIO_RCON_PASSWORD:-factorio}"

if [ ! -f /app/data/saves/save.zip ]; then
    echo "Save file does not exist, creating a new one..."
    /app/factorio/bin/x64/factorio --config /app/config.ini \
        --map-gen-settings /app/map-gen-settings.json \
        --map-settings /app/map-settings.json \
        --create /app/data/saves/save.zip
else
    echo "Save file exists, reusing it"
fi

FACTORIO_GAME_PORT="${FACTORIO_GAME_PORT:-34197}"
FACTORIO_BIND_ADDRESS="${FACTORIO_BIND_ADDRESS:-0.0.0.0}"

echo "Starting headless dedicated server on ${FACTORIO_BIND_ADDRESS}:${FACTORIO_GAME_PORT} with rcon on port $FACTORIO_RCON_PORT"
stopping=0
server_pid=
stop_server() {
    stopping=1
    if [ -n "$server_pid" ]; then
        kill "$server_pid" 2>/dev/null || true
    fi
}
trap stop_server TERM INT

while [ "$stopping" -eq 0 ]; do
    /app/factorio/bin/x64/factorio \
        --config /app/config.ini \
        --start-server /app/data/saves/save.zip \
        --server-settings /app/server-settings.json \
        --disable-migration-window \
        --port "$FACTORIO_GAME_PORT" \
        --bind "$FACTORIO_BIND_ADDRESS" \
        --rcon-port "$FACTORIO_RCON_PORT" \
        --rcon-password "$FACTORIO_RCON_PASSWORD" &
    server_pid=$!
    if wait "$server_pid"; then
        status=0
    else
        status=$?
    fi
    server_pid=
    [ "$stopping" -eq 1 ] && exit 0
    echo "Factorio server exited with status $status; restarting in 5 seconds"
    sleep 5 &
    server_pid=$!
    if wait "$server_pid"; then
        :
    fi
    server_pid=
done