package state

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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

// OpenOptions controls lifecycle work performed while opening a database.
// Hosted processes leave migrations to the deployment's explicit migration step.
type OpenOptions struct {
	Migrate bool
}

// Open opens SQLite or Postgres based on backend/url.
func Open(backend, dbURL, root string) (*DB, error) {
	return OpenWithOptions(backend, dbURL, root, OpenOptions{Migrate: true})
}

// OpenWithOptions opens SQLite or Postgres with an explicit migration policy.
func OpenWithOptions(backend, dbURL, root string, options OpenOptions) (*DB, error) {
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
		if options.Migrate {
			if err := db.Migrate(context.Background()); err != nil {
				_ = sqlDB.Close()
				return nil, err
			}
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
		configurePostgresPool(sqlDB)
		if err := sqlDB.Ping(); err != nil {
			_ = sqlDB.Close()
			return nil, fmt.Errorf("postgres ping: %w", err)
		}
		db := &DB{SQL: sqlDB, Dialect: "postgres", Root: root}
		if options.Migrate {
			if err := db.Migrate(context.Background()); err != nil {
				_ = sqlDB.Close()
				return nil, err
			}
		}
		return db, nil
	default:
		return nil, fmt.Errorf("unsupported state backend: %s", backend)
	}
}

// VerifySchema fails fast when a hosted process starts before the migration job
// has applied the schema expected by this binary.
func (db *DB) VerifySchema(ctx context.Context) error {
	var version int
	err := db.SQL.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version)
	if err != nil {
		return fmt.Errorf("read schema version (run `ves control-plane migrate` first): %w", err)
	}
	expected := latestMigrationVersion()
	if version != expected {
		return fmt.Errorf("database schema version %d does not match binary version %d; run `ves control-plane migrate`", version, expected)
	}
	return nil
}

func (db *DB) Close() error {
	if db == nil || db.SQL == nil {
		return nil
	}
	return db.SQL.Close()
}

func (db *DB) Migrate(ctx context.Context) error {
	if db.Dialect == "postgres" {
		conn, err := db.SQL.Conn(ctx)
		if err != nil {
			return fmt.Errorf("acquire migration lock connection: %w", err)
		}
		defer conn.Close()
		if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtext('vessica-control-plane-schema'))`); err != nil {
			return fmt.Errorf("acquire migration lock: %w", err)
		}
		defer func() {
			_, _ = conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock(hashtext('vessica-control-plane-schema'))`)
		}()
	}
	return db.migrate(ctx)
}

func (db *DB) migrate(ctx context.Context) error {
	schema := SchemaSQL
	if db.Dialect == "postgres" {
		schema = adaptPostgres(schema)
	}
	if _, err := db.SQL.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}
	if err := db.applyMigrations(ctx); err != nil {
		return fmt.Errorf("apply ordered migrations: %w", err)
	}
	// Knowledge memories moved to the authoritative knowledge store. There are
	// no compatibility obligations for the experimental legacy table.
	if db.Dialect == "sqlite" {
		_, _ = db.SQL.ExecContext(ctx, `DROP TABLE IF EXISTS memory_fts`)
	}
	if _, err := db.SQL.ExecContext(ctx, `DROP TABLE IF EXISTS memories`); err != nil {
		return fmt.Errorf("remove legacy memory table: %w", err)
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

func configurePostgresPool(db *sql.DB) {
	maxOpen := envInt("VES_DB_MAX_OPEN_CONNS", 20)
	maxIdle := envInt("VES_DB_MAX_IDLE_CONNS", 5)
	if maxIdle > maxOpen {
		maxIdle = maxOpen
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(time.Duration(envInt("VES_DB_CONN_MAX_LIFETIME_SECONDS", 1800)) * time.Second)
	db.SetConnMaxIdleTime(time.Duration(envInt("VES_DB_CONN_MAX_IDLE_SECONDS", 300)) * time.Second)
}

func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return fallback
	}
	return value
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
