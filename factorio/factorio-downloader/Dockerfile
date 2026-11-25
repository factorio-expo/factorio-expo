FROM debian:trixie-20261020-slim

RUN apt-get update && apt-get upgrade -y && apt-get install -y --no-install-recommends \
    curl tar xz-utils ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
