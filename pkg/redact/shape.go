package redact

import (
	"bytes"
	"encoding/json"
	"regexp"
)

// Redaction by key name cannot see a credential that sits in an ordinary
// field. A key-value store returns one under `value`, a log line carries one
// mid-sentence under `logs` — nothing about either name says "secret", and
// both are real cases that reached a published solution.
//
// Matching the shape of the secret itself covers those. It is the complement
// of IsSecretKey, not a replacement: a field NAMED apiKey should be hidden
// whatever it holds, and a value that IS a key should be hidden whatever it
// is called.
//
// Anchored on issued-token prefixes rather than an entropy heuristic. A false
// positive silently corrupts data someone relies on, and "looks random"
// describes plenty of legitimate identifiers — pod names, UUIDs, commit
// hashes.

var credentialShapes = []*regexp.Regexp{
	// Short bound on purpose: a log line usually carries a TRUNCATED key, and
	// a leading fragment is still disclosure. Nothing legitimate is shaped
	// like this prefix, so there is no false positive to trade against.
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{6,}`),                                        // Anthropic
	regexp.MustCompile(`sk-(?:proj-)?[A-Za-z0-9_\-]{20,}`),                                 // OpenAI
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`),                                       // GitHub
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`),                                     // GitHub fine-grained
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),                                                 // AWS access key id
	regexp.MustCompile(`ASIA[0-9A-Z]{16}`),                                                 // AWS temporary
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9\-]{10,}`),                                    // Slack
	regexp.MustCompile(`AIza[0-9A-Za-z_\-]{35}`),                                           // Google API
	regexp.MustCompile(`eyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}`), // JWT
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9_\-.=]{20,}`),                               // Authorization header
}

// TextByShape masks every credential-shaped run in a string, reporting
// whether anything changed.
func TextByShape(s string) (string, bool) {
	out := s
	for _, re := range credentialShapes {
		out = re.ReplaceAllString(out, Value)
	}
	return out, out != s
}

// SecretsByShape walks a decoded JSON value and masks credential-shaped
// strings wherever they sit — in any field, at any depth, and inside prose.
// The input is mutated in place for maps and slices, matching how callers
// already hold decoded payloads; use JSONByShape when working from bytes.
func SecretsByShape(v interface{}) (interface{}, bool) {
	switch t := v.(type) {
	case string:
		return TextByShape(t)
	case []interface{}:
		changed := false
		for i, item := range t {
			replaced, did := SecretsByShape(item)
			if did {
				t[i] = replaced
				changed = true
			}
		}
		return t, changed
	case map[string]interface{}:
		changed := false
		for k, item := range t {
			replaced, did := SecretsByShape(item)
			if did {
				t[k] = replaced
				changed = true
			}
		}
		return t, changed
	}
	return v, false
}

// JSONByShape masks credential-shaped values in a JSON payload, returning the
// payload to keep and whether anything was masked.
//
// A payload that does not parse is still scrubbed as text rather than passed
// through: the point is that nothing credential-shaped survives, whatever
// shape it arrived in. A payload that parses but cannot be re-encoded is
// dropped, because publishing the original is the outcome this exists to
// prevent.
//
// Bytes with nothing credential-shaped are returned exactly as given, so
// ordinary data round-trips untouched.
func JSONByShape(data []byte) ([]byte, bool) {
	if len(data) == 0 {
		return data, false
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	// Keep numeric literals as their original text, so re-encoding a payload
	// that does hold a credential cannot shift an int64 through float64.
	dec.UseNumber()

	var decoded interface{}
	if err := dec.Decode(&decoded); err != nil {
		masked, changed := TextByShape(string(data))
		if !changed {
			return data, false
		}
		return []byte(masked), true
	}

	replaced, changed := SecretsByShape(decoded)
	if !changed {
		return data, false
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	// Without this the marker is escaped to <redacted>, which is
	// harder to read than the thing it replaced.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(replaced); err != nil {
		return nil, true
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), true
}
