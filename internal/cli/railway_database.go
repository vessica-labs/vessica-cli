package cli

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/vessica-labs/vessica-cli/internal/config"
)

const (
	controlDatabaseName   = "vessica_control"
	knowledgeDatabaseName = "vessica_knowledge"
	controlDatabaseRole   = "vessica_control_user"
	knowledgeDatabaseRole = "vessica_knowledge_user"
)

type railwayDatabaseURLs struct {
	Control   string
	Knowledge string
}

func ensureRailwayDatabases(ctx context.Context, cfg config.Config, secrets railwaySecrets) (railwayDatabaseURLs, error) {
	variables, err := waitForRailwayDatabaseVariables(ctx, cfg, 2*time.Minute)
	if err != nil {
		return railwayDatabaseURLs{}, err
	}
	privateAdminURL := firstNonEmpty(variables["DATABASE_URL"])
	publicAdminURL := firstNonEmpty(variables["DATABASE_PUBLIC_URL"], privateAdminURL)
	if privateAdminURL == "" || publicAdminURL == "" {
		return railwayDatabaseURLs{}, fmt.Errorf("Railway Postgres service did not expose DATABASE_URL and DATABASE_PUBLIC_URL")
	}
	urls, err := deriveRailwayDatabaseURLs(privateAdminURL, secrets)
	if err != nil {
		return railwayDatabaseURLs{}, err
	}
	if err := bootstrapRailwayDatabases(ctx, publicAdminURL, secrets); err != nil {
		return railwayDatabaseURLs{}, err
	}
	return urls, nil
}

func bootstrapRailwayDatabases(ctx context.Context, publicAdminURL string, secrets railwaySecrets) error {
	if secrets.ControlDatabasePassword == "" || secrets.KnowledgeDatabasePassword == "" {
		return fmt.Errorf("logical database credentials are unavailable")
	}
	admin, err := sql.Open("pgx", publicAdminURL)
	if err != nil {
		return fmt.Errorf("open Railway Postgres bootstrap connection: %w", err)
	}
	defer admin.Close()
	conn, err := admin.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire Railway Postgres bootstrap connection: %w", err)
	}
	defer conn.Close()
	if err := conn.PingContext(ctx); err != nil {
		return fmt.Errorf("connect to Railway Postgres public endpoint: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtext('vessica-logical-database-bootstrap'))`); err != nil {
		return fmt.Errorf("lock Railway Postgres bootstrap: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock(hashtext('vessica-logical-database-bootstrap'))`)
	}()
	for _, database := range []struct {
		name, role, password string
	}{
		{controlDatabaseName, controlDatabaseRole, secrets.ControlDatabasePassword},
		{knowledgeDatabaseName, knowledgeDatabaseRole, secrets.KnowledgeDatabasePassword},
	} {
		if err := ensurePostgresRole(ctx, conn, database.role, database.password); err != nil {
			return err
		}
		if err := ensurePostgresDatabase(ctx, conn, database.name, database.role); err != nil {
			return err
		}
	}
	if err := configureKnowledgeDatabase(ctx, publicAdminURL); err != nil {
		return err
	}
	return nil
}

func waitForRailwayDatabaseVariables(ctx context.Context, cfg config.Config, timeout time.Duration) (map[string]string, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		variables, err := readRailwayVariables(ctx, cfg, cfg.Hosted.PostgresServiceID)
		if err == nil && variables["DATABASE_URL"] != "" {
			return variables, nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("DATABASE_URL is not ready")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return nil, fmt.Errorf("Railway Postgres variables did not become ready: %w", lastErr)
}

func deriveRailwayDatabaseURLs(adminURL string, secrets railwaySecrets) (railwayDatabaseURLs, error) {
	if secrets.ControlDatabasePassword == "" || secrets.KnowledgeDatabasePassword == "" {
		return railwayDatabaseURLs{}, fmt.Errorf("logical database credentials are unavailable")
	}
	derive := func(database, role, password string) (string, error) {
		parsed, err := url.Parse(adminURL)
		if err != nil {
			return "", fmt.Errorf("parse Railway Postgres URL: %w", err)
		}
		if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
			return "", fmt.Errorf("unsupported Railway Postgres URL scheme %q", parsed.Scheme)
		}
		if parsed.Host == "" {
			return "", fmt.Errorf("Railway Postgres URL is missing a host")
		}
		parsed.User = url.UserPassword(role, password)
		parsed.Path = "/" + database
		parsed.RawPath = ""
		return parsed.String(), nil
	}
	controlURL, err := derive(controlDatabaseName, controlDatabaseRole, secrets.ControlDatabasePassword)
	if err != nil {
		return railwayDatabaseURLs{}, err
	}
	knowledgeURL, err := derive(knowledgeDatabaseName, knowledgeDatabaseRole, secrets.KnowledgeDatabasePassword)
	if err != nil {
		return railwayDatabaseURLs{}, err
	}
	return railwayDatabaseURLs{Control: controlURL, Knowledge: knowledgeURL}, nil
}

func databasePasswordFromURL(rawURL, expectedRole string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.User == nil || parsed.User.Username() != expectedRole {
		return ""
	}
	password, _ := parsed.User.Password()
	return password
}

type postgresBootstrapConnection interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func ensurePostgresRole(ctx context.Context, admin postgresBootstrapConnection, role, password string) error {
	var exists bool
	if err := admin.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)`, role).Scan(&exists); err != nil {
		return fmt.Errorf("inspect Postgres role %s: %w", role, err)
	}
	statement := "CREATE ROLE " + quotePostgresIdentifier(role) + " LOGIN PASSWORD " + quotePostgresLiteral(password)
	if exists {
		statement = "ALTER ROLE " + quotePostgresIdentifier(role) + " LOGIN PASSWORD " + quotePostgresLiteral(password)
	}
	if _, err := admin.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("ensure Postgres role %s: %w", role, err)
	}
	return nil
}

func ensurePostgresDatabase(ctx context.Context, admin postgresBootstrapConnection, database, role string) error {
	var exists bool
	if err := admin.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1)`, database).Scan(&exists); err != nil {
		return fmt.Errorf("inspect Postgres database %s: %w", database, err)
	}
	if !exists {
		statement := "CREATE DATABASE " + quotePostgresIdentifier(database) + " OWNER " + quotePostgresIdentifier(role)
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create Postgres database %s: %w", database, err)
		}
	}
	var adminRole string
	if err := admin.QueryRowContext(ctx, `SELECT current_user`).Scan(&adminRole); err != nil {
		return fmt.Errorf("resolve Postgres bootstrap role: %w", err)
	}
	statements := []string{
		"ALTER DATABASE " + quotePostgresIdentifier(database) + " OWNER TO " + quotePostgresIdentifier(role),
		"REVOKE CONNECT ON DATABASE " + quotePostgresIdentifier(database) + " FROM PUBLIC",
		"GRANT CONNECT ON DATABASE " + quotePostgresIdentifier(database) + " TO " + quotePostgresIdentifier(role),
		"GRANT CONNECT ON DATABASE " + quotePostgresIdentifier(database) + " TO " + quotePostgresIdentifier(adminRole),
	}
	for _, statement := range statements {
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure Postgres database %s: %w", database, err)
		}
	}
	return nil
}

func configureKnowledgeDatabase(ctx context.Context, adminURL string) error {
	parsed, err := url.Parse(adminURL)
	if err != nil {
		return fmt.Errorf("parse Railway Postgres URL for knowledge bootstrap: %w", err)
	}
	parsed.Path = "/" + knowledgeDatabaseName
	parsed.RawPath = ""
	knowledgeAdmin, err := sql.Open("pgx", parsed.String())
	if err != nil {
		return fmt.Errorf("open knowledge database bootstrap connection: %w", err)
	}
	defer knowledgeAdmin.Close()
	if _, err := knowledgeAdmin.ExecContext(ctx, `CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
		return fmt.Errorf("enable pgvector in %s: %w", knowledgeDatabaseName, err)
	}
	return nil
}

func quotePostgresIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func quotePostgresLiteral(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}
