#!/bin/sh
set -eu

mkdir -p /app/data/mods /app/data/script-output/previews

# Multiplayer requires identical mods on server and client, so the client image
# ships and refreshes the same bridge mod. Factorio manages mod-list.json itself
# and enables newly discovered mods by default.
rm -rf /app/data/mods/bp-preview-bridge_*
rm -rf /app/data/mods/bp-preview-bridge
rm -rf /app/data/mods/factorio-expo-bridge_*
rm -rf /app/data/mods/factorio-expo-bridge
cp -r /app/mod/factorio-expo-bridge /app/data/mods/

# This server is configured for unauthenticated LAN multiplayer. Remove any
# credentials left in the persistent client volume by older configurations.
rm -f /app/data/player-data.json

# llvmpipe's JIT can crash under emulated CPUs (e.g. amd64 container on Apple
# Silicon via QEMU/Rosetta). Auto-detecting emulation reliably from inside the
# container isn't feasible, so it's controlled explicitly via GALLIUM_DRIVER.
# Set GALLIUM_DRIVER=softpipe in the environment (e.g. docker-compose.yaml) on
# machines where the binary is emulated; leave it unset on native x86_64 hosts.
if [ -n "${GALLIUM_DRIVER:-}" ]; then
    echo "Using GALLIUM_DRIVER=$GALLIUM_DRIVER"
fi

FACTORIO_SERVER_HOST="${FACTORIO_SERVER_HOST:?set FACTORIO_SERVER_HOST}"
FACTORIO_SERVER_PORT="${FACTORIO_SERVER_PORT:-34197}"

# --- Watchdog: what the client log says about the connection.
#
# Factorio keeps the client process running in every multiplayer state, also
# after it lost or never reached the server, so an exit alone never restarts a
# stuck client. Each state change is logged as
#   Info ClientMultiplayerManager.cpp:610: UpdateTick(1593) changing state from(WaitingForCommandToStartSendingTickClosures) to(InGame)
# States seen with Factorio 2.1.21 and 2.1.24:
#   InGame                                     joined; the only good state
#   WaitingForUserToSaveOrQuitAfterServerLeft  the server crashed, restarted or
#                                              stopped while in game (~30 s later)
#   InitializationFailed                       the server's host is unknown (its
#                                              container is down) or did not
#                                              answer within 10 s
#   Disconnected                               refused: "UserWithThatNameAlreadyInGame"
#                                              while the server still holds the
#                                              previous connection (~20 s after
#                                              a client crash)
# Any state after InGame means the client left the game. A watchdog next to
# each run stops the client when it left the game, failed to join, has not
# joined `join_timeout` seconds after it began, or logged no state at all
# `start_timeout` seconds after starting (sprite loading without the atlas
# cache is the slow part); the loop below then starts a new one.
client_log=/app/data/factorio-current.log
previous_log=/app/data/factorio-previous.log
failed_states="InitializationFailed Disconnected Failed WaitingForUserToSaveOrQuitAfterServerLeft"
watch_interval=2
start_timeout=900
join_timeout=120
stop_grace=10
# Seconds between client runs: a run that joined restarts after the first
# delay, each run in a row that never joined doubles it up to the last one.
restart_delay_min=5
restart_delay_max=60

# The client's latest multiplayer state; empty before the first change.
client_state() {
    sed -n 's/.*ClientMultiplayerManager\.cpp:[0-9]*: .*changing state from([A-Za-z]*) to(\([A-Za-z]*\)).*/\1/p' \
        "$client_log" 2>/dev/null | tail -n 1
}

# Whether this run of the client was in the game at some point.
client_joined() {
    grep -q 'ClientMultiplayerManager\.cpp:[0-9]*: .*changing state from([A-Za-z]*) to(InGame)' "$client_log" 2>/dev/null
}

is_failed_state() {
    for failed in $failed_states; do
        if [ "$1" = "$failed" ]; then
            return 0
        fi
    done
    return 1
}

# Sends signal $1 to every process named $2...; this container runs only the
# client, so names are enough.
signal_processes() {
    signal=$1
    shift
    for comm in /proc/[0-9]*/comm; do
        IFS= read -r name 2>/dev/null < "$comm" || continue
        for wanted in "$@"; do
            if [ "$name" = "$wanted" ]; then
                pid=${comm#/proc/}
                kill "-$signal" "${pid%/comm}" 2>/dev/null || true
            fi
        done
    done
}

# Runs in the background next to one client run until the client is stopped
# (by this watchdog or otherwise) and the loop kills it.
watch_client() {
    started=$(date +%s)
    joining_since=
    while :; do
        sleep "$watch_interval"
        now=$(date +%s)
        state=$(client_state)
        reason=
        if [ "$state" = InGame ]; then
            # Once in the game the frame-rate limiter (fpslimit.so) may kick in.
            : > /tmp/fpslimit.on
            continue
        elif [ -z "$state" ]; then
            if [ $((now - started)) -ge "$start_timeout" ]; then
                reason="no multiplayer state ${start_timeout}s after starting"
            fi
        elif client_joined; then
            reason="left the game ($state)"
        elif is_failed_state "$state"; then
            reason="could not join the game ($state)"
        else
            joining_since=${joining_since:-$now}
            if [ $((now - joining_since)) -ge "$join_timeout" ]; then
                reason="not in the game ${join_timeout}s after it began to join ($state)"
            fi
        fi
        if [ -n "$reason" ]; then
            break
        fi
    done
    echo "Factorio client watchdog: $reason; stopping the client"
    # xvfb-run stops Xvfb once Factorio exits; both are killed if Factorio
    # ignores the request (or never started, xvfb-run waiting for Xvfb).
    signal_processes TERM factorio
    sleep "$stop_grace"
    echo "Factorio client watchdog: the client did not stop; killing it"
    signal_processes KILL factorio Xvfb xvfb-run
}

echo "Connecting to multiplayer game at ${FACTORIO_SERVER_HOST}:${FACTORIO_SERVER_PORT}"
# Not using `exec` here: xvfb-run relies on Xvfb signalling readiness (SIGUSR1)
# to its direct parent process. When that parent is PID 1, the signal can be
# lost under some container/emulation setups, causing xvfb-run to hang
# indefinitely. Running it as a plain child process avoids that.
# -a picks the first free display number instead of failing on a busy one.
stopping=0
client_pid=
watchdog_pid=
restart_delay=$restart_delay_min
stop_client() {
    stopping=1
    if [ -n "$watchdog_pid" ]; then
        kill "$watchdog_pid" 2>/dev/null || true
    fi
    if [ -n "$client_pid" ]; then
        kill "$client_pid" 2>/dev/null || true
    fi
}
trap stop_client TERM INT

while [ "$stopping" -eq 0 ]; do
    # This container owns its X displays exclusively, so leftover state from a
    # crashed client is stale and safe to clear before retrying.
    rm -f /tmp/.X*-lock /tmp/fpslimit.on
    rm -rf /tmp/.X11-unix
    # Factorio rotates its log on start; rotating first means the watchdog
    # never reads the previous run's states.
    if [ -f "$client_log" ]; then
        mv -f "$client_log" "$previous_log"
    fi

    LD_PRELOAD=/app/fpslimit.so xvfb-run -a -s "-screen 0 ${FACTORIO_CLIENT_SCREEN:-640x360x24}" /app/factorio/bin/x64/factorio \
        --disable-audio \
        --config /app/config.ini \
        --disable-migration-window \
        --mp-connect "${FACTORIO_SERVER_HOST}:${FACTORIO_SERVER_PORT}" &
    client_pid=$!
    watch_client &
    watchdog_pid=$!
    if wait "$client_pid"; then
        status=0
    else
        status=$?
    fi
    client_pid=
    kill "$watchdog_pid" 2>/dev/null || true
    wait "$watchdog_pid" 2>/dev/null || true
    watchdog_pid=
    [ "$stopping" -eq 1 ] && exit 0

    if client_joined; then
        restart_delay=$restart_delay_min
    fi
    echo "Factorio client exited with status $status; restarting in $restart_delay seconds"
    sleep "$restart_delay" &
    client_pid=$!
    if wait "$client_pid"; then
        :
    fi
    client_pid=
    restart_delay=$((restart_delay * 2))
    if [ "$restart_delay" -gt "$restart_delay_max" ]; then
        restart_delay=$restart_delay_max
    fi
done
