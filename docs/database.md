# Database tooling

PostgreSQL is the only database. The one genuinely nested structure (the book
tree) is stored as shape-only `jsonb` (`revisions.tree`, see [schema.md](schema.md));
everything else is relational (tags, favorites, collections, uniqueness),
and entry + revision + rows + dispatch intents must commit in one transaction.
MongoDB was rejected: no gain over jsonb pointers, plus a second engine to
operate. Revisit only with measured access or scale problems. Images never go
in PostgreSQL.

## Stack

- **Migrations**: [golang-migrate](https://github.com/golang-migrate/migrate). Each app owns plain `.sql` up/down files for its own database: `apps/expo-api/migrations/` (catalog) and `apps/expo-render-worker/migrations/` (runner). Neither app runs DDL itself. The runner refuses to start on an unmigrated database.
- **Tooling** (`apps/expo-db/`): one image with `psql`, `migrate` and both migration sets.
  - `expo-db-setup` runs on every `docker compose up`, before the apps start. It creates or updates the roles and the runner database, applies pending **up** migrations to both databases and refreshes grants. It never migrates down. The app roles carry `statement_timeout = 60s` and `idle_in_transaction_session_timeout = 5min` (`ALTER ROLE` in `lib.sh`); the superuser keeps the server defaults. Server settings (memory sized for a single shared host) are in `apps/expo-db/postgres/postgresql.conf`, copied into the `expo-postgres` image. A runner database still in the old layout, where the worker created its own tables (`runner_schema_migrations`), is dropped and rebuilt; its queue is disposable.
  - `docker compose run --rm expo-db-migrate` is interactive: it asks for the database (catalog/runner), then up, down (number of steps or all, confirmed by typing the database name) or force (repairs a dirty version). Arguments skip the questions: `expo-db-migrate runner down 1`.
- **Queries**: [sqlc](https://sqlc.dev/), SQL in `apps/expo-api/internal/db/queries/`, config `internal/db/sqlc.yaml`, generated code in `internal/db/generated/` (checked in, never hand-edited). The sqlc binary is not checked in: install v1.31.1 and run `sqlc generate` from `apps/expo-api/internal/db/`.
- **Driver**: [pgx](https://github.com/jackc/pgx) v5 (sqlc `sql/pgx/v5` mode).
- Rejected: GORM/ent (want plain reviewable SQL), goose (equivalent to golang-migrate).

## Local development

`expo-postgres` (`postgres:18.6-alpine3.24` plus `apps/expo-db/postgres/postgresql.conf`, named volume `postgres-data` mounted at
`/var/lib/postgresql`; PostgreSQL 18 images keep `PGDATA` in a versioned
subdirectory, `/var/lib/postgresql/18/docker`) is published on `127.0.0.1:5432`
so `psql` or an IDE can inspect data; it is not reachable from other machines.
Containers use `expo-postgres:5432`. The `expo-db` tooling image uses the same
major version so `pg_dump`/`psql` match the server.

### Major version upgrades

A new PostgreSQL major cannot open the old data directory. Dump with the old
image before switching, then restore into a fresh volume:

```sh
docker compose exec expo-postgres pg_dumpall -U factorio_expo > expo-pg.sql
docker compose down && docker volume rm factorio-expo_postgres-data
docker compose up -d expo-postgres
docker compose exec -T expo-postgres psql -U factorio_expo -d postgres < expo-pg.sql
docker compose up -d
```

Typesense needs no dump: it is a projection, so drop `typesense-data` if a
new version cannot read it and let the API rebuild the index.

### Roles

Roles in the one PostgreSQL instance:

| Role | Env | Can |
|---|---|---|
| superuser | `POSTGRES_USER`/`POSTGRES_PASSWORD` | Everything; used only by `expo-db-*` and local inspection. Owns all tables. |
| API | `API_POSTGRES_USER`/`API_POSTGRES_PASSWORD` | Connect to the catalog DB (`POSTGRES_DB`), DML on its tables. No DDL, no `schema_migrations`. |
| runner | `RUNNER_POSTGRES_USER`/`RUNNER_POSTGRES_PASSWORD` | Connect to the runner DB (`RUNNER_POSTGRES_DB`), DML on its tables. No DDL, no `schema_migrations`. |

Neither app role can connect to the other database. Grants are refreshed
after every up migration, and default privileges cover tables that new
migrations create.

## Migration policy

Every schema change is a new numbered migration with an up and a down file
(`000003_...`, and so on). An applied migration is never edited. `000001_init`
is the baseline.

The catalog's `000002_seed_initial_data` holds the initial curated tag groups,
tags and tag suggestion rules and the reserved usernames, as data only, kept
apart from the schema so both stay readable. Later changes to those rows are new
numbered migrations too. Migrations are applied in order by `expo-db-setup`.
Staff edit the same rows in the console (tags, suggestion rules, reserved
usernames); those edits live in the database, so a lasting default for new
installs also goes into a migration.

golang-migrate keeps no checksums, so an existing database silently keeps its old schema: recreate
it when you want a clean dev database: `docker compose down`, then
`docker volume rm factorio-expo_postgres-data factorio-expo_typesense-data`
(not `down -v`, which also drops the downloaded game). To wipe the whole dev stack, also
remove the `render-worker-data` and `factorio-preview-inbox` volumes and the preview
files in `EXPO_STORAGE_DIR`.

For database-backed Go tests see the README.
