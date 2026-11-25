#!/bin/sh
# Runs on every `docker compose up` (expo-db-setup) before expo-api and
# expo-render-worker start: ensures roles and databases, applies pending up
# migrations for both databases and refreshes the app roles' grants. Never
# migrates down; use expo-db-migrate for that.
. /usr/local/lib/expo-db/lib.sh

wait_for_postgres
ensure_roles_and_databases
reset_legacy_runner_schema
for target in catalog runner; do
	echo "expo-db: migrating $target ($(db_name "$target")) up"
	migrate_db "$target" up
	grant_app_role "$target"
done
echo "expo-db: ready"
