// Package db inicializa el pool de pgx y aplica migraciones embebidas.
//
// Las migraciones se cargan desde internal/db/sql/*.sql usando embed.FS
// y se ejecutan al arranque, con control de versión en una tabla
// interna `schema_migrations`.
package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed sql/*.sql
var migrationsFS embed.FS

// Pool envuelve un pgxpool.Pool con un helper para migraciones.
type Pool struct {
	*pgxpool.Pool
}

// Open abre un pool y lo verifica con un ping.
func Open(ctx context.Context, dsn string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	cfg.MaxConns = 20
	cfg.MinConns = 2
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("new pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &Pool{Pool: pool}, nil
}

// Migrate aplica todas las migraciones pendientes en orden lexicográfico.
//
// Crea la tabla `schema_migrations(version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ)`
// si no existe. Cada archivo `.sql` se ejecuta en su propia transacción.
func (p *Pool) Migrate(ctx context.Context) error {
	if _, err := p.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, "sql")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	applied := map[string]bool{}
	rows, err := p.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("read applied: %w", err)
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()

	for _, name := range files {
		version := strings.TrimSuffix(name, ".sql")
		if applied[version] {
			continue
		}
		body, err := fs.ReadFile(migrationsFS, "sql/"+name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if err := p.runMigration(ctx, version, string(body)); err != nil {
			return err
		}
	}
	return nil
}

func (p *Pool) runMigration(ctx context.Context, version, body string) error {
	tx, err := p.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin %s: %w", version, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	if _, err = tx.Exec(ctx, body); err != nil {
		return fmt.Errorf("exec %s: %w", version, err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ($1)`, version); err != nil {
		return fmt.Errorf("record %s: %w", version, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit %s: %w", version, err)
	}
	return nil
}

// WithinTx ejecuta fn dentro de una transacción. Si fn retorna error,
// hace rollback; si no, commit.
func (p *Pool) WithinTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := p.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if rec := recover(); rec != nil {
			_ = tx.Rollback(ctx)
			panic(rec)
		}
	}()
	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			return fmt.Errorf("%w (rollback error: %v)", err, rbErr)
		}
		return err
	}
	return tx.Commit(ctx)
}