package bundle

import (
	"testing"
)

func TestURL(t *testing.T) {
	t.Setenv("RELEASE_NAME", "myrelease")
	got, err := URL("tei")
	if err != nil {
		t.Fatalf("URL: unexpected error: %v", err)
	}
	want := "http://myrelease-tei:80"
	if got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
}

func TestURLMissingRelease(t *testing.T) {
	t.Setenv("RELEASE_NAME", "")
	if _, err := URL("tei"); err == nil {
		t.Fatal("URL: want error when RELEASE_NAME is unset")
	}
}

func TestURLOr(t *testing.T) {
	t.Setenv("RELEASE_NAME", "")
	if got := URLOr("tei", "http://localhost:8080"); got != "http://localhost:8080" {
		t.Errorf("URLOr fallback = %q, want fallback", got)
	}
	t.Setenv("RELEASE_NAME", "rel")
	if got := URLOr("tei", "http://localhost:8080"); got != "http://rel-tei:80" {
		t.Errorf("URLOr = %q, want in-cluster URL", got)
	}
}

func TestNamespaceRelease(t *testing.T) {
	t.Setenv("RELEASE_NAMESPACE", "default")
	t.Setenv("RELEASE_NAME", "rel")
	if Namespace() != "default" {
		t.Errorf("Namespace = %q, want %q", Namespace(), "default")
	}
	if Release() != "rel" {
		t.Errorf("Release = %q, want %q", Release(), "rel")
	}
}

func TestPostgresDSN(t *testing.T) {
	// Missing creds → clear error (bundle disabled / outside chart).
	t.Setenv("RELEASE_NAME", "rel")
	if _, err := PostgresDSN("pgvector"); err == nil {
		t.Fatal("expected error without credential env")
	}

	// Full env → DSN with deterministic host + escaped password.
	t.Setenv("BUNDLE_PGVECTOR_USER", "tinysystems")
	t.Setenv("BUNDLE_PGVECTOR_PASSWORD", "p@ss/w:rd")
	t.Setenv("BUNDLE_PGVECTOR_DB", "tinysystems")
	dsn, err := PostgresDSN("pgvector")
	if err != nil {
		t.Fatalf("PostgresDSN: %v", err)
	}
	want := "postgres://tinysystems:p%40ss%2Fw%3Ard@rel-pgvector:5432/tinysystems?sslmode=disable"
	if dsn != want {
		t.Errorf("dsn = %q, want %q", dsn, want)
	}

	// Explicit host override wins.
	t.Setenv("BUNDLE_PGVECTOR_HOST", "external-pg.example.com")
	dsn, _ = PostgresDSN("pgvector")
	if dsn != "postgres://tinysystems:p%40ss%2Fw%3Ard@external-pg.example.com:5432/tinysystems?sslmode=disable" {
		t.Errorf("host override not honored: %q", dsn)
	}
}
