// Package migrations holds the SQL migration files and exposes a
// minimal embedded runner so deployments don't need a separate
// `migrate` binary.
package migrations

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

//go:embed *.sql
var fs embed.FS

const tableDDL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version  TEXT PRIMARY KEY,
    checksum TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`

// Up applies every `*.up.sql` migration that hasn't been applied yet,
// in lexicographic order. Each migration runs inside its own
// transaction. If a previously-applied migration's checksum has
// changed since application, Up returns an error rather than
// silently re-running.
func Up(ctx context.Context, conn *pgx.Conn) error {
	if _, err := conn.Exec(ctx, tableDDL); err != nil {
		return fmt.Errorf("migrations: bootstrap table: %w", err)
	}

	entries, err := fs.ReadDir(".")
	if err != nil {
		return fmt.Errorf("migrations: read embed: %w", err)
	}
	var versions []string
	files := map[string][]byte{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		body, err := fs.ReadFile(name)
		if err != nil {
			return fmt.Errorf("migrations: read %s: %w", name, err)
		}
		version := strings.TrimSuffix(name, ".up.sql")
		versions = append(versions, version)
		files[version] = body
	}
	sort.Strings(versions)

	applied, err := readApplied(ctx, conn)
	if err != nil {
		return err
	}

	for _, v := range versions {
		body := files[v]
		sum := checksum(body)
		if prev, ok := applied[v]; ok {
			if prev != sum {
				return fmt.Errorf(
					"migrations: %s checksum drift (applied=%s, local=%s)",
					v, prev, sum,
				)
			}
			continue
		}
		if err := apply(ctx, conn, v, sum, string(body)); err != nil {
			return fmt.Errorf("migrations: apply %s: %w", v, err)
		}
	}
	return nil
}

func readApplied(ctx context.Context, conn *pgx.Conn) (map[string]string, error) {
	rows, err := conn.Query(ctx, "SELECT version, checksum FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("migrations: list applied: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var v, c string
		if err := rows.Scan(&v, &c); err != nil {
			return nil, err
		}
		out[v] = c
	}
	return out, rows.Err()
}

func apply(ctx context.Context, conn *pgx.Conn, version, sum, body string) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, body); err != nil {
		return fmt.Errorf("exec: %w", err)
	}
	_, err = tx.Exec(
		ctx,
		"INSERT INTO schema_migrations(version, checksum) VALUES ($1, $2)",
		version, sum,
	)
	if err != nil {
		return fmt.Errorf("record: %w", err)
	}
	return tx.Commit(ctx)
}

func checksum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// ErrNoMigrations is returned when no `.up.sql` files were embedded.
var ErrNoMigrations = errors.New("migrations: no .up.sql files embedded")
