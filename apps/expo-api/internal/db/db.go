// Package db owns the Postgres connection pool and the sqlc-generated
// query layer. Callers get a *pgxpool.Pool plus generated typed queries from
// the generated subpackage; no ORM or hand-rolled SQL string building lives
// here.
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect opens a pgx connection pool against databaseURL and verifies
// connectivity with a ping before returning.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}
