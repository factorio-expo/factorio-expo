FROM debian:trixie-20261104-slim AS fpslimit
RUN apt-get update && apt-get upgrade -y && apt-get install -y --no-install-recommends gcc libc6-dev     && rm -rf /var/lib/apt/lists/*
COPY factorio/factorio-client/fpslimit/fpslimit.c /src/fpslimit.c
RUN gcc -shared -fPIC -O2 -o /fpslimit.so /src/fpslimit.c -ldl

FROM debian:trixie-20261104-slim
RUN apt-get update && apt-get upgrade -y && apt-get install -y --no-install-recommends \
    xvfb xauth \
    && rm -rf /var/lib/apt/lists/*
RUN mkdir -p /app
COPY --from=fpslimit /fpslimit.so /app/fpslimit.so
COPY factorio/factorio-client/config.ini /app/config.ini
COPY factorio/factorio-expo-bridge /app/mod/factorio-expo-bridge
COPY factorio/factorio-healthcheck.sh /factorio-healthcheck.sh
COPY factorio/factorio-client/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh /factorio-healthcheck.sh
ENTRYPOINT [ "/entrypoint.sh" ]
