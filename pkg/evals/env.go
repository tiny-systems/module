package evals

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// Credentials for an eval come from the environment, never from the file.
//
// An eval fires a real trigger on a real cluster, so a flow that calls a
// provider needs a real key. Both obvious places to put one are wrong: the
// eval file is committed, and a node's settings are the Spec, which travels
// with every export. Nine flows were once found holding the same live key that
// second way.
//
// So the value is named in the file and supplied at fire time. It reaches the
// trigger as transient port data — the same path a person pressing Send takes —
// and is written down nowhere:
//
//	trigger:
//	  node: signal-f2b7b
//	  data:
//	    send: true
//	    context:
//	      apiKey: ${ANTHROPIC_API_KEY}
//
// This lives in the SDK because both hosts must behave identically. An eval
// that resolves a credential locally and silently fires without one on the
// platform is worse than an eval that fails.

var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// ExpandEnv replaces ${VAR} references in the trigger payload with values from
// the environment, in place.
//
// An unset variable is an error naming every one that is missing, so a person
// fixes their environment in one pass. It is deliberately not a warning: firing
// with an empty credential fails three hops downstream with an authentication
// error, and whoever reads that goes looking for a broken flow instead of an
// unset variable.
//
// A variable exported as empty is set. That is a choice — "no credential here" —
// and distinct from never having been exported.
func (s *Spec) ExpandEnv() error {
	if s == nil || len(s.Trigger.Data) == 0 {
		return nil
	}

	missing := map[string]bool{}
	expanded := expandValue(s.Trigger.Data, missing)

	if len(missing) > 0 {
		names := make([]string, 0, len(missing))
		for n := range missing {
			names = append(names, n)
		}
		sort.Strings(names)
		// Names only. An eval failure gets pasted into issues and CI logs, so
		// nothing that resolved may appear here.
		return fmt.Errorf("%s: unset environment variable(s): %s",
			s.Name, strings.Join(names, ", "))
	}

	m, _ := expanded.(map[string]interface{})
	s.Trigger.Data = m
	return nil
}

func expandValue(v interface{}, missing map[string]bool) interface{} {
	switch n := v.(type) {
	case string:
		if !strings.Contains(n, "${") {
			return n
		}
		return envRef.ReplaceAllStringFunc(n, func(ref string) string {
			name := envRef.FindStringSubmatch(ref)[1]
			val, ok := os.LookupEnv(name)
			if !ok {
				missing[name] = true
				return ref
			}
			return val
		})

	case map[string]interface{}:
		out := make(map[string]interface{}, len(n))
		for k, child := range n {
			out[k] = expandValue(child, missing)
		}
		return out

	case []interface{}:
		out := make([]interface{}, len(n))
		for i, child := range n {
			out[i] = expandValue(child, missing)
		}
		return out
	}
	return v
}
