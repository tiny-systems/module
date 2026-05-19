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
