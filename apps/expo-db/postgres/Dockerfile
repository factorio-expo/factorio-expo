# expo-postgres: the official image with our server settings
# (postgresql.conf next to this file). Bump the tag for security fixes and
# rebuild with `docker compose build --pull`.
FROM postgres:18.6-alpine3.24

COPY postgresql.conf /etc/postgresql/postgresql.conf

# config_file replaces the postgresql.conf initdb wrote into the data
# directory; pg_hba.conf stays there.
CMD ["postgres", "-c", "config_file=/etc/postgresql/postgresql.conf"]
