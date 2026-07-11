package state

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// DB wraps a SQL database with dialect helpers.
type DB struct {
	SQL       *sql.DB
	Dialect   string // sqlite | postgres
	Root      string
	Workspace *Workspace
}

// Open opens SQLite or Postgres based on backend/url.
func Open(backend, dbURL, root string) (*DB, error) {
	switch backend {
	case "sqlite", "":
		path := dbURL
		if path == "" {
			path = filepath.Join(root, ".vessica", "state", "vessica.db")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
		sqlDB, err := sql.Open("sqlite", dsn)
		if err != nil {
			return nil, err
		}
		sqlDB.SetMaxOpenConns(4)
		sqlDB.SetMaxIdleConns(4)
		db := &DB{SQL: sqlDB, Dialect: "sqlite", Root: root}
		if err := db.Migrate(context.Background()); err != nil {
			_ = sqlDB.Close()
			return nil, err
		}
		return db, nil
	case "postgres-url", "postgres", "postgres-docker":
		if dbURL == "" {
			return nil, fmt.Errorf("state.db_url is required for postgres backend")
		}
		sqlDB, err := sql.Open("pgx", dbURL)
		if err != nil {
			return nil, err
		}
		if err := sqlDB.Ping(); err != nil {
			_ = sqlDB.Close()
			return nil, fmt.Errorf("postgres ping: %w", err)
		}
		db := &DB{SQL: sqlDB, Dialect: "postgres", Root: root}
		if err := db.Migrate(context.Background()); err != nil {
			_ = sqlDB.Close()
			return nil, err
		}
		return db, nil
	default:
		return nil, fmt.Errorf("unsupported state backend: %s", backend)
	}
}

func (db *DB) Close() error {
	if db == nil || db.SQL == nil {
		return nil
	}
	return db.SQL.Close()
}

func (db *DB) Migrate(ctx context.Context) error {
	schema := SchemaSQL
	if db.Dialect == "postgres" {
		schema = adaptPostgres(schema)
	}
	if _, err := db.SQL.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}
	if db.Dialect == "sqlite" {
		if _, err := db.SQL.ExecContext(ctx, SchemaFTSSQLite); err != nil {
			return fmt.Errorf("migrate fts: %w", err)
		}
	}
	if err := db.ensureColumn(ctx, "tickets", "source_run_id", "TEXT"); err != nil {
		return err
	}
	if err := db.ensureColumn(ctx, "waves", "source_run_id", "TEXT"); err != nil {
		return err
	}
	if err := db.ensureColumn(ctx, "runs", "model", "TEXT"); err != nil {
		return err
	}
	if err := db.ensureColumn(ctx, "runs", "reasoning_effort", "TEXT"); err != nil {
		return err
	}
	if err := db.ensureColumn(ctx, "sandboxes", "last_accessed_at", "TEXT"); err != nil {
		return err
	}
	if err := db.ensureColumn(ctx, "sandboxes", "expires_at", "TEXT"); err != nil {
		return err
	}
	if err := db.ensureColumn(ctx, "sandboxes", "retained_until", "TEXT"); err != nil {
		return err
	}
	for _, column := range []struct{ name, typ string }{
		{"sync_status", "TEXT NOT NULL DEFAULT 'pending'"},
		{"external_version", "TEXT"},
		{"last_synced_at", "TEXT"},
		{"last_error", "TEXT"},
	} {
		if err := db.ensureColumn(ctx, "external_mappings", column.name, column.typ); err != nil {
			return err
		}
	}
	if _, err := db.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_tickets_source_run ON tickets(source_run_id)`); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_waves_source_run ON waves(source_run_id)`); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_sandboxes_expires ON sandboxes(expires_at)`); err != nil {
		return err
	}
	var n int
	err := db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = 1`).Scan(&n)
	if err != nil {
		return err
	}
	if n == 0 {
		_, err = db.Exec(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES (1, ?)`, Now())
		if err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) ensureColumn(ctx context.Context, table, column, typ string) error {
	exists := false
	if db.Dialect == "postgres" {
		err := db.SQL.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2
		)`, table, column).Scan(&exists)
		if err != nil {
			return err
		}
	} else {
		rows, err := db.SQL.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var cid int
			var name, colType string
			var notNull int
			var dflt any
			var pk int
			if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
				return err
			}
			if name == column {
				exists = true
				break
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
	}
	if exists {
		return nil
	}
	_, err := db.Exec(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, typ))
	return err
}

func adaptPostgres(s string) string {
	return s
}

func (db *DB) Rebind(q string) string {
	if db.Dialect != "postgres" {
		return q
	}
	var b strings.Builder
	n := 0
	for i := 0; i < len(q); i++ {
		if q[i] == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(fmt.Sprintf("%d", n))
		} else {
			b.WriteByte(q[i])
		}
	}
	return b.String()
}

func (db *DB) Exec(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return db.SQL.ExecContext(ctx, db.Rebind(q), args...)
}

func (db *DB) Query(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return db.SQL.QueryContext(ctx, db.Rebind(q), args...)
}

func (db *DB) QueryRow(ctx context.Context, q string, args ...any) *sql.Row {
	return db.SQL.QueryRowContext(ctx, db.Rebind(q), args...)
}

func (db *DB) Begin(ctx context.Context) (*sql.Tx, error) {
	return db.SQL.BeginTx(ctx, nil)
}

func (db *DB) Ping(ctx context.Context) error {
	return db.SQL.PingContext(ctx)
}
