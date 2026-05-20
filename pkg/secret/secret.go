// Package secret resolves `[[secret:<name>/<key>]]` placeholders in
// component settings against Kubernetes Secrets in the module pod's
// own namespace. Components call Resolve on their settings struct
// inside OnSettings, before reading any field that might contain a
// secret reference.
//
// The placeholder uses `[[...]]` rather than `{{...}}` because the
// platform's ajson expression evaluator runs over settings JSON
// first and would try to parse `[[secret:foo/bar]]` as an ajson
// expression — failing on the colon and replacing the field with
// nil before this resolver got a chance to substitute.
//
// The resolver walks the supplied struct via reflection, looking for
// string fields whose value matches `[[secret:<name>/<key>]]`. Each
// match is fetched from the cluster via the supplied K8sClient and
// substituted in place. Fields that don't match the pattern are
// untouched.
//
// Why manual:
//   - Explicit at call site — readers can grep `secret.Resolve` to
//     find every place a secret enters component memory.
//   - No hidden K8s API calls inside the runner. The component owns
//     the lifecycle: when to resolve, when to re-resolve, what to
//     do on failure (most callers should fail loud — see Resolve's
//     error contract).
//   - The K8sClient injection point (ClientAware.OnClient) already
//     exists; no new SDK hook required.
//
// Rotation: this resolver does not watch for changes. To pick up a
// rotated Secret, the component must re-trigger OnSettings (e.g. by
// re-sending the settings message, or restarting the pod). Watch-
// based invalidation is a deliberate follow-up — first ship the
// simple version, add cache + watch once usage patterns are clear.
package secret

import (
	"context"
	"fmt"
	"reflect"
	"regexp"

	"github.com/tiny-systems/module/module"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// placeholderRe matches `[[secret:<name>/<key>]]` where <name> is the
// Secret resource name and <key> is a key inside the Secret's data
// map. Both must match [A-Za-z0-9._-]+. The placeholder must be the
// entire string content of the field — no substring substitution, no
// partial matches. That keeps the resolver simple and avoids the
// "value looks like a placeholder by coincidence" failure mode.
var placeholderRe = regexp.MustCompile(`^\[\[secret:([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+)\]\]$`)

// Resolve walks settings via reflection and replaces every
// `[[secret:<name>/<key>]]` string field with the corresponding value
// from a Kubernetes Secret in the K8sClient's namespace.
//
// settings must be a pointer to a struct (or pointer to anything that
// transitively contains string fields). Passing a non-pointer is a
// programmer error and returns an error so it surfaces in tests
// rather than silently no-oping.
//
// Errors:
//   - K8s API failures (Secret not found, RBAC denied) bubble up. The
//     caller decides whether to fail loud (recommended) or fall back.
//   - Malformed placeholders are ignored — a field containing literal
//     `[[secret:bad]]` (missing slash) stays unchanged.
//   - Missing keys inside a Secret produce an error. Better to fail
//     at OnSettings than at request time.
func Resolve(ctx context.Context, settings any, client module.K8sClient) error {
	if client == nil {
		return fmt.Errorf("secret.Resolve: K8sClient is nil — component must implement ClientAware and wait for OnClient before resolving")
	}
	v := reflect.ValueOf(settings)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return fmt.Errorf("secret.Resolve: settings must be a non-nil pointer, got %T", settings)
	}
	return walk(ctx, v.Elem(), client)
}

func walk(ctx context.Context, v reflect.Value, client module.K8sClient) error {
	switch v.Kind() {
	case reflect.String:
		if !v.CanSet() {
			return nil
		}
		s := v.String()
		match := placeholderRe.FindStringSubmatch(s)
		if match == nil {
			return nil
		}
		name, key := match[1], match[2]
		value, err := fetch(ctx, client, name, key)
		if err != nil {
			return err
		}
		v.SetString(value)
		return nil
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			if !f.CanSet() {
				continue
			}
			if err := walk(ctx, f, client); err != nil {
				return err
			}
		}
		return nil
	case reflect.Ptr, reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return walk(ctx, v.Elem(), client)
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if err := walk(ctx, v.Index(i), client); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		// Map values aren't directly settable via reflection. For each
		// entry we either substitute (string match) or recurse and
		// write the mutated value back. This is the path that hits
		// `Settings.Context any` after JSON unmarshalling: the outer
		// value's Kind is Interface, walking it yields a Map whose
		// values are themselves Interface-wrapped — we unwrap to find
		// the underlying string and SetMapIndex with the resolved
		// value.
		iter := v.MapRange()
		// Materialise keys first so we can mutate the map inside the
		// loop without invalidating the iterator.
		keys := make([]reflect.Value, 0, v.Len())
		for iter.Next() {
			keys = append(keys, iter.Key())
		}
		for _, mk := range keys {
			mv := v.MapIndex(mk)
			actual := mv
			if actual.Kind() == reflect.Interface {
				actual = actual.Elem()
			}
			if actual.Kind() == reflect.String {
				match := placeholderRe.FindStringSubmatch(actual.String())
				if match == nil {
					continue
				}
				value, err := fetch(ctx, client, match[1], match[2])
				if err != nil {
					return err
				}
				v.SetMapIndex(mk, reflect.ValueOf(value))
				continue
			}
			if actual.Kind() == reflect.Map || actual.Kind() == reflect.Slice || actual.Kind() == reflect.Struct {
				// Make a settable copy, walk into it, write back.
				tmp := reflect.New(actual.Type()).Elem()
				tmp.Set(actual)
				if err := walk(ctx, tmp, client); err != nil {
					return err
				}
				v.SetMapIndex(mk, tmp)
			}
		}
		return nil
	}
	return nil
}

func fetch(ctx context.Context, client module.K8sClient, name, key string) (string, error) {
	c := client.GetK8sClient()
	ns := client.GetNamespace()
	if ns == "" {
		return "", fmt.Errorf("secret.Resolve: K8sClient.GetNamespace returned empty — module pod must run with a namespace context")
	}
	var sec corev1.Secret
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &sec); err != nil {
		return "", fmt.Errorf("secret.Resolve: get Secret %s/%s: %w", ns, name, err)
	}
	raw, ok := sec.Data[key]
	if !ok {
		return "", fmt.Errorf("secret.Resolve: key %q not found in Secret %s/%s", key, ns, name)
	}
	return string(raw), nil
}
