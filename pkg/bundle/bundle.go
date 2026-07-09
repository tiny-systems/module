// Package bundle exposes service discovery for the third-party
// Helm releases a module declared as bundles via
// registry.SetRequirements. The operator chart provisions each
// enabled bundle as a subchart aliased to the bundle's declared
// name, so the Service ends up at <release>-<bundle>.<ns>.svc...
// — a deterministic location the module pod can resolve at startup.
//
// The release/namespace come from RELEASE_NAME and RELEASE_NAMESPACE
// env vars the operator chart injects via .Release.Name /
// .Release.Namespace at deploy time. Components consume them through
// this package rather than reading env directly — keeps the
// convention in one place and makes future changes (e.g. moving to
// per-bundle Secret reads for credentialed services) localised.
package bundle

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

// DefaultPort is what http-ish bundle subcharts expose on. TEI's
// subchart uses port 80; future HTTP bundles will follow suit. For
// bundles on non-default ports (pgvector → 5432, redis → 6379)
// callers use the helpers in this package that know each protocol.
const DefaultPort = 80

// URL returns the in-cluster HTTP endpoint of a bundle by its
// declared name. Assumes the bundle subchart published a Service at
// <release>-<name> in the same namespace as the module pod. Returns
// an error if RELEASE_NAME isn't set — that means the module is
// running outside the operator chart and the caller should either
// fall back to a manually-configured URL or fail loudly.
func URL(name string) (string, error) {
	release, ok := os.LookupEnv("RELEASE_NAME")
	if !ok || release == "" {
		return "", fmt.Errorf("bundle.URL(%q): RELEASE_NAME env not set — module not running inside the operator chart? Set a manual override via the component's settings.baseURL", name)
	}
	return fmt.Sprintf("http://%s-%s:%d", release, name, DefaultPort), nil
}

// URLOr returns URL(name) if RELEASE_NAME is set, otherwise the
// supplied fallback. Useful in component code that wants
// zero-config in-cluster operation but also needs to support local
// dev / external endpoints without erroring.
func URLOr(name, fallback string) string {
	if url, err := URL(name); err == nil {
		return url
	}
	return fallback
}

// PostgresDSN returns a connection string for a credentialed Postgres
// bundle (e.g. "pgvector"). Unlike URL(), a database needs credentials,
// which the operator chart injects as env when the bundle is enabled —
// the password sourced from the bundle's own Secret, never from values:
//
//	BUNDLE_<NAME>_HOST     (optional; defaults to <release>-<name>)
//	BUNDLE_<NAME>_USER
//	BUNDLE_<NAME>_PASSWORD (secretKeyRef into the bundle's Secret)
//	BUNDLE_<NAME>_DB
//
// Returns an error when the credential env is absent — the bundle is
// disabled or the module runs outside the chart — so callers can fall
// back to a user-supplied DSN or fail with a clear message.
func PostgresDSN(name string) (string, error) {
	prefix := "BUNDLE_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	user := os.Getenv(prefix + "_USER")
	pass := os.Getenv(prefix + "_PASSWORD")
	db := os.Getenv(prefix + "_DB")
	if user == "" || pass == "" || db == "" {
		return "", fmt.Errorf("bundle.PostgresDSN(%q): %s_USER/_PASSWORD/_DB env not set — bundle disabled or module not running inside the operator chart; supply a DSN explicitly", name, prefix)
	}
	host := os.Getenv(prefix + "_HOST")
	if host == "" {
		release, ok := os.LookupEnv("RELEASE_NAME")
		if !ok || release == "" {
			return "", fmt.Errorf("bundle.PostgresDSN(%q): RELEASE_NAME env not set and no %s_HOST override", name, prefix)
		}
		host = fmt.Sprintf("%s-%s", release, name)
	}
	// sslmode=disable: in-cluster ClusterIP service, same namespace.
	return fmt.Sprintf("postgres://%s:%s@%s:5432/%s?sslmode=disable",
		url.QueryEscape(user), url.QueryEscape(pass), host, url.PathEscape(db)), nil
}

// Namespace returns the namespace the module pod is running in.
// Mirrors RELEASE_NAMESPACE injected by the operator chart. Empty
// string when running outside the chart (use sparingly — most
// callers should rely on URL() instead, which encodes the lookup).
func Namespace() string {
	return os.Getenv("RELEASE_NAMESPACE")
}

// Release returns the helm release name the module pod is part of.
// Mirrors RELEASE_NAME injected by the operator chart.
func Release() string {
	return os.Getenv("RELEASE_NAME")
}
