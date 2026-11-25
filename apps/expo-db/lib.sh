# Shared helpers for expo-db-setup and expo-db-migrate. Connects as the
# PostgreSQL superuser (POSTGRES_USER), which owns every table; the apps use
# restricted roles that may only read and write rows.
#
# Each logical database has a name, a migrations directory and an app role:
#   catalog: POSTGRES_DB,        /migrations/catalog, API_POSTGRES_USER
#   runner:  RUNNER_POSTGRES_DB, /migrations/runner,  RUNNER_POSTGRES_USER

set -eu

: "${POSTGRES_USER:?set POSTGRES_USER}"
: "${POSTGRES_PASSWORD:?set POSTGRES_PASSWORD}"
: "${POSTGRES_DB:?set POSTGRES_DB}"
: "${API_POSTGRES_USER:?set API_POSTGRES_USER}"
: "${API_POSTGRES_PASSWORD:?set API_POSTGRES_PASSWORD}"
: "${RUNNER_POSTGRES_USER:?set RUNNER_POSTGRES_USER}"
: "${RUNNER_POSTGRES_PASSWORD:?set RUNNER_POSTGRES_PASSWORD}"
: "${RUNNER_POSTGRES_DB:?set RUNNER_POSTGRES_DB}"
POSTGRES_HOST="${POSTGRES_HOST:-expo-postgres}"

export PGHOST="$POSTGRES_HOST" PGUSER="$POSTGRES_USER" PGPASSWORD="$POSTGRES_PASSWORD" PGDATABASE="$POSTGRES_DB"

die() {
	echo "expo-db: $*" >&2
	exit 1
}

db_name() {
	case "$1" in
	catalog) echo "$POSTGRES_DB" ;;
	runner) echo "$RUNNER_POSTGRES_DB" ;;
	*) die "unknown database '$1' (want catalog or runner)" ;;
	esac
}

db_role() {
	case "$1" in
	catalog) echo "$API_POSTGRES_USER" ;;
	runner) echo "$RUNNER_POSTGRES_USER" ;;
	*) die "unknown database '$1' (want catalog or runner)" ;;
	esac
}

# urlencode percent-encodes everything but unreserved URL characters, so
# passwords with symbols survive the migrate connection URL.
urlencode() {
	rest=$1
	out=
	while [ -n "$rest" ]; do
		char=${rest%"${rest#?}"}
		rest=${rest#?}
		case "$char" in
		[a-zA-Z0-9._~-]) out="$out$char" ;;
		*) out="$out$(printf '%%%02X' "'$char")" ;;
		esac
	done
	printf '%s' "$out"
}

# migrate_db <catalog|runner> <golang-migrate args...>
migrate_db() {
	target=$1
	shift
	url="postgres://$(urlencode "$POSTGRES_USER"):$(urlencode "$POSTGRES_PASSWORD")@$POSTGRES_HOST:5432/$(db_name "$target")?sslmode=disable"
	migrate -path "/migrations/$target" -database "$url" "$@"
}

wait_for_postgres() {
	tries=0
	until pg_isready -q; do
		tries=$((tries + 1))
		[ "$tries" -lt 30 ] || die "postgres at $POSTGRES_HOST is not ready"
		sleep 1
	done
}

# ensure_roles_and_databases creates or updates both app roles and the runner
# database, and limits who may connect to each database. Idempotent.
ensure_roles_and_databases() {
	for role in "$API_POSTGRES_USER" "$RUNNER_POSTGRES_USER"; do
		[ "$role" != "$POSTGRES_USER" ] || die "app roles must differ from POSTGRES_USER"
	done
	[ "$API_POSTGRES_USER" != "$RUNNER_POSTGRES_USER" ] || die "API and runner roles must differ"
	[ "$RUNNER_POSTGRES_DB" != "$POSTGRES_DB" ] || die "runner and catalog databases must differ"

	psql -q -v ON_ERROR_STOP=1 \
		-v api_user="$API_POSTGRES_USER" -v api_password="$API_POSTGRES_PASSWORD" \
		-v runner_user="$RUNNER_POSTGRES_USER" -v runner_password="$RUNNER_POSTGRES_PASSWORD" \
		-v catalog_db="$POSTGRES_DB" -v runner_db="$RUNNER_POSTGRES_DB" <<'SQL'
SELECT format('CREATE ROLE %I LOGIN', :'api_user')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'api_user')\gexec
ALTER ROLE :"api_user" WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD :'api_password';

SELECT format('CREATE ROLE %I LOGIN', :'runner_user')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'runner_user')\gexec
ALTER ROLE :"runner_user" WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD :'runner_password';

SELECT format('CREATE DATABASE %I', :'runner_db')
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = :'runner_db')\gexec

-- Each app role may connect only to its own database.
REVOKE ALL ON DATABASE :"catalog_db" FROM PUBLIC;
REVOKE ALL ON DATABASE :"runner_db" FROM PUBLIC;
GRANT CONNECT, TEMPORARY ON DATABASE :"catalog_db" TO :"api_user";
GRANT CONNECT, TEMPORARY ON DATABASE :"runner_db" TO :"runner_user";

-- Bounds for the app roles, so a runaway query or a transaction left open by
-- a bug cannot hold locks or a connection forever. The superuser (setup,
-- migrations, manual work) keeps the server defaults.
ALTER ROLE :"api_user" SET statement_timeout = '60s';
ALTER ROLE :"api_user" SET idle_in_transaction_session_timeout = '5min';
ALTER ROLE :"runner_user" SET statement_timeout = '60s';
ALTER ROLE :"runner_user" SET idle_in_transaction_session_timeout = '5min';
SQL
}

# reset_legacy_runner_schema drops runner tables that the worker created
# itself before it had migration files (marked by runner_schema_migrations),
# including a dirty migrate state left by colliding with them. Runner state is
# disposable: the API re-dispatches jobs the runner no longer knows (job_lost).
reset_legacy_runner_schema() {
	legacy=$(psql -tA -d "$RUNNER_POSTGRES_DB" -c "SELECT to_regclass('public.runner_schema_migrations') IS NOT NULL")
	[ "$legacy" = "t" ] || return 0
	echo "expo-db: dropping legacy self-migrated runner tables"
	psql -q -v ON_ERROR_STOP=1 -d "$RUNNER_POSTGRES_DB" <<'SQL'
DROP TABLE IF EXISTS runner_artifacts, runner_jobs, runner_schema_migrations, schema_migrations;
SQL
}

# grant_app_role <catalog|runner> gives the app role row access (no DDL) to
# every current and future table and sequence, except migration bookkeeping.
grant_app_role() {
	psql -q -v ON_ERROR_STOP=1 -d "$(db_name "$1")" -v app_role="$(db_role "$1")" <<'SQL'
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO :"app_role";
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO :"app_role";
GRANT USAGE, SELECT, UPDATE ON ALL SEQUENCES IN SCHEMA public TO :"app_role";
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO :"app_role";
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT, UPDATE ON SEQUENCES TO :"app_role";
SELECT format('REVOKE ALL ON TABLE schema_migrations FROM %I', :'app_role')
WHERE to_regclass('public.schema_migrations') IS NOT NULL\gexec
SQL
}
