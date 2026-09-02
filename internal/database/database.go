package database

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

const bootstrapSQL = `CREATE TABLE IF NOT EXISTS schema_migrations (
  version integer PRIMARY KEY,
  applied_at timestamptz NOT NULL DEFAULT now()
);`

type migration struct {
	version int
	name    string
	body    string
}

// loadMigrations returns every embedded migration ordered by version. File names
// are "<version>_<name>.sql"; the version is the ordering and the identity that
// schema_migrations records, so a file is applied at most once per database.
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, err
	}
	items := make([]migration, 0, len(entries))
	seen := map[int]string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		if !ok {
			return nil, fmt.Errorf("migration %q must be named <version>_<name>.sql", entry.Name())
		}
		version, convErr := strconv.Atoi(prefix)
		if convErr != nil || version < 1 {
			return nil, fmt.Errorf("migration %q has an invalid version prefix", entry.Name())
		}
		if duplicate, exists := seen[version]; exists {
			return nil, fmt.Errorf("migrations %q and %q share version %d", duplicate, entry.Name(), version)
		}
		body, readErr := fs.ReadFile(migrationFS, "migrations/"+entry.Name())
		if readErr != nil {
			return nil, readErr
		}
		seen[version] = entry.Name()
		items = append(items, migration{version: version, name: entry.Name(), body: string(body)})
	}
	sort.Slice(items, func(a, b int) bool { return items[a].version < items[b].version })
	return items, nil
}

// Migrate applies every migration the database has not recorded yet. Each file
// runs inside its own transaction together with its schema_migrations row, so a
// failing migration leaves neither partial schema nor a false version record.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, bootstrapSQL); err != nil {
		return fmt.Errorf("prepare schema_migrations: %w", err)
	}
	items, err := loadMigrations()
	if err != nil {
		return err
	}
	applied := map[int]bool{}
	rows, err := pool.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			rows.Close()
			return err
		}
		applied[version] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range items {
		if applied[item.version] {
			continue
		}
		tx, beginErr := pool.Begin(ctx)
		if beginErr != nil {
			return beginErr
		}
		if _, execErr := tx.Exec(ctx, item.body); execErr != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", item.name, execErr)
		}
		if _, execErr := tx.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES($1) ON CONFLICT DO NOTHING`, item.version); execErr != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", item.name, execErr)
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return fmt.Errorf("commit migration %s: %w", item.name, commitErr)
		}
	}
	return nil
}

// SchemaVersion reports the highest applied migration version.
func SchemaVersion(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	var version int
	err := pool.QueryRow(ctx, `SELECT COALESCE(max(version),0) FROM schema_migrations`).Scan(&version)
	return version, err
}

// ExpectedSchemaVersion reports the highest version shipped with this binary.
func ExpectedSchemaVersion() int {
	items, err := loadMigrations()
	if err != nil || len(items) == 0 {
		return 0
	}
	return items[len(items)-1].version
}

func Open(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("configure postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate postgres: %w", err)
	}
	return pool, nil
}

// OpenWithRetry allows the service and PostgreSQL to start together without
// relying on a container orchestrator's restart policy.
func OpenWithRetry(ctx context.Context, dsn string, timeout time.Duration) (*pgxpool.Pool, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		pool, err := Open(ctx, dsn)
		if err == nil {
			return pool, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return nil, lastErr
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func IsConstraint(err error, name string) bool {
	return err != nil && strings.Contains(err.Error(), name)
}
