FROM debian:trixie-20261020-slim
RUN apt-get update && apt-get upgrade -y && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /app
COPY factorio/factorio-server/config.ini /app/config.ini
COPY factorio/factorio-server/map-gen-settings.json /app/map-gen-settings.json
COPY factorio/factorio-server/map-settings.json /app/map-settings.json
COPY factorio/factorio-server/server-settings.json /app/server-settings.json
COPY factorio/factorio-expo-bridge /app/mod/factorio-expo-bridge
COPY factorio/factorio-healthcheck.sh /factorio-healthcheck.sh
COPY factorio/factorio-server/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh /factorio-healthcheck.sh
ENTRYPOINT [ "/entrypoint.sh" ]