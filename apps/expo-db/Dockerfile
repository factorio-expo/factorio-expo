# Database tooling: role/database setup plus golang-migrate for every app's
# migrations. Used by the expo-db-setup (automatic) and expo-db-migrate
# (interactive) compose services.
FROM migrate/migrate:v4.20.1 AS migrate

FROM postgres:18.6-alpine3.24

COPY --from=migrate /usr/local/bin/migrate /usr/local/bin/migrate
COPY apps/expo-api/migrations /migrations/catalog
COPY apps/expo-render-worker/migrations /migrations/runner
COPY apps/expo-db/lib.sh /usr/local/lib/expo-db/lib.sh
COPY apps/expo-db/setup.sh /usr/local/bin/expo-db-setup
COPY apps/expo-db/migrate.sh /usr/local/bin/expo-db-migrate
RUN chmod +x /usr/local/bin/expo-db-setup /usr/local/bin/expo-db-migrate

ENTRYPOINT []
CMD ["expo-db-setup"]
