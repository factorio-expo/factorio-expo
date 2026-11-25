#!/bin/sh
# Interactive migrations for the catalog (expo-api) and runner
# (expo-render-worker) databases:
#
#   docker compose run --rm expo-db-migrate
#
# Asks for the database and the action. Arguments skip the questions:
#
#   docker compose run --rm expo-db-migrate catalog up
#   docker compose run --rm expo-db-migrate runner down 1
#   docker compose run --rm expo-db-migrate runner force 1
. /usr/local/lib/expo-db/lib.sh

# One stream for menus, prompts and migrate output keeps them in order.
exec 1>&2

target=${1:-}
action=${2:-}
argument=${3:-}

ask() {
	printf '%s ' "$1" >&2
	read -r answer || die "no input (run with: docker compose run --rm expo-db-migrate)"
	printf '%s' "$answer"
}

show_version() {
	printf 'Version of %s (%s): ' "$target" "$database"
	migrate_db "$target" version 2>&1 | sed 's/^error: no migration$/none/' || true
}

wait_for_postgres

if [ -z "$target" ]; then
	echo "Which database?"
	echo "  1) catalog  - expo-api ($POSTGRES_DB)"
	echo "  2) runner   - expo-render-worker ($RUNNER_POSTGRES_DB)"
	case "$(ask '>')" in
	1 | catalog) target=catalog ;;
	2 | runner) target=runner ;;
	*) die "pick 1 or 2" ;;
	esac
fi
database=$(db_name "$target")
show_version

if [ -z "$action" ]; then
	echo "What should happen?"
	echo "  1) up    - apply every pending migration"
	echo "  2) down  - revert migrations"
	echo "  3) force - record a version without running SQL (repairs a dirty state)"
	case "$(ask '>')" in
	1 | up) action=up ;;
	2 | down) action=down ;;
	3 | force) action=force ;;
	*) die "pick 1, 2 or 3" ;;
	esac
fi

case "$action" in
up)
	ensure_roles_and_databases
	migrate_db "$target" up
	grant_app_role "$target"
	;;
down)
	if [ -z "$argument" ]; then
		argument=$(ask "How many migrations to revert? (number or 'all', default 1):")
		argument=${argument:-1}
	fi
	case "$argument" in
	all) steps=-all ;;
	'' | 0 | *[!0-9]*) die "steps must be a positive number or 'all'" ;;
	*) steps=$argument ;;
	esac
	echo "This reverts $argument migration(s) of $database and can drop data."
	[ "$(ask "Type the database name ($database) to continue:")" = "$database" ] || die "aborted"
	migrate_db "$target" down "$steps"
	;;
force)
	if [ -z "$argument" ]; then
		argument=$(ask "Version to record:")
	fi
	migrate_db "$target" force "$argument"
	;;
*) die "unknown action '$action' (want up, down or force)" ;;
esac

show_version
