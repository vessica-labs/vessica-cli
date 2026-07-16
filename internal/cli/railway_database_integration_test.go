package cli

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"testing"
)

func TestRailwayLogicalDatabaseBootstrapIntegration(t *testing.T) {
	adminURL := os.Getenv("TEST_POSTGRES_ADMIN_URL")
	if adminURL == "" {
		t.Skip("TEST_POSTGRES_ADMIN_URL is not set")
	}
	secrets := railwaySecrets{
		ControlDatabasePassword:   "vessica_control_dev",
		KnowledgeDatabasePassword: "vessica_knowledge_dev",
	}
	ctx := context.Background()
	for attempt := 0; attempt < 2; attempt++ {
		if err := bootstrapRailwayDatabases(ctx, adminURL, secrets); err != nil {
			t.Fatalf("bootstrap attempt %d: %v", attempt+1, err)
		}
	}
	urls, err := deriveRailwayDatabaseURLs(adminURL, secrets)
	if err != nil {
		t.Fatal(err)
	}
	for name, rawURL := range map[string]string{"control": urls.Control, "knowledge": urls.Knowledge} {
		db, err := sql.Open("pgx", rawURL)
		if err != nil {
			t.Fatalf("open %s database: %v", name, err)
		}
		if err := db.PingContext(ctx); err != nil {
			db.Close()
			t.Fatalf("ping %s database: %v", name, err)
		}
		var vectorInstalled bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname='vector')`).Scan(&vectorInstalled); err != nil {
			db.Close()
			t.Fatal(err)
		}
		if vectorInstalled != (name == "knowledge") {
			db.Close()
			t.Fatalf("%s vector installed=%v", name, vectorInstalled)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
	for name, rawURL := range map[string]string{
		"control role to knowledge database": replaceDatabaseInURL(t, urls.Control, knowledgeDatabaseName),
		"knowledge role to control database": replaceDatabaseInURL(t, urls.Knowledge, controlDatabaseName),
	} {
		db, err := sql.Open("pgx", rawURL)
		if err != nil {
			continue
		}
		if err := db.PingContext(ctx); err == nil {
			db.Close()
			t.Fatalf("%s unexpectedly connected", name)
		}
		db.Close()
	}
}

func replaceDatabaseInURL(t *testing.T, rawURL, database string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + database
	parsed.RawPath = ""
	return parsed.String()
}
