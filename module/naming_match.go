package module

import "strings"

// NameMatches reports whether a module name found on a resource identifies the
// module running under `running`.
//
// A publisher prefix is optional in BOTH directions, and '/' and '-' are the
// same separator. So "http-module-v0", "tinysystems/http-module-v0" and
// "tinysystems-http-module-v0" all identify the same module.
//
// This exists because a module's identity and its publisher were conflated. A
// node's name embeds the module it needs, and the platform used to build that
// from the publishing workspace's slug, so the same module installed from two
// places — or from a workspace that was later renamed — produced names that no
// longer matched the operator answering for it. The reconciler compared with
// string equality and silently ignored anything that differed, which is the
// worst possible failure for a node: it exists, it renders, it never runs.
//
// Publisher belongs in the catalog, not in cluster state. New references are
// written bare; this keeps every reference already out there working, so no
// node has to be renamed and no flow has to be rebuilt.
func NameMatches(found, running string) bool {
	f := normalizeModuleSeparators(found)
	r := normalizeModuleSeparators(running)
	if f == "" || r == "" {
		return false
	}
	if f == r {
		return true
	}
	// Bare found, qualified running: "http-module-v0" must match an operator
	// answering as "tinysystems-http-module-v0".
	if strings.HasSuffix(r, "-"+f) {
		return true
	}
	// Qualified found, bare running: a node written before the prefix was
	// dropped, against an operator installed without one.
	return strings.HasSuffix(f, "-"+r)
}

// normalizeModuleSeparators lowercases and treats '/' as '-', so
// publisher/module and publisher-module are the same string.
func normalizeModuleSeparators(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "/", "-"))
}
