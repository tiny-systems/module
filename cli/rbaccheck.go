package cli

import (
	"fmt"
	"go/ast"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// This file answers a question nothing else asks: does the module's declared
// RBAC actually cover the Kubernetes calls its code makes?
//
// The drift gate compares the published overlay against the module's own
// declaration, so the two can agree perfectly while both omit a verb the code
// needs. That is not hypothetical — pod_create called Create and pod_delete
// called Delete for months while kubernetes-module declared neither, and both
// would have failed with a 403 on any self-hosted install. Nothing caught it
// because nothing compared the declaration against the source.
//
// Types are resolved rather than pattern-matched, because the resource is
// carried by the argument (`Create(ctx, job)`), not by the call name. Grep
// cannot tell a Job from a Pod.

// clientVerbs maps controller-runtime client methods to the RBAC verb each
// needs. DeleteAllOf needs both: it lists to find its targets.
var clientVerbs = map[string][]string{
	"Get":         {"get"},
	"List":        {"list"},
	"Watch":       {"watch"},
	"Create":      {"create"},
	"Delete":      {"delete"},
	"Update":      {"update"},
	"Patch":       {"patch"},
	"DeleteAllOf": {"delete", "list"},
}

// baseAccess is what EnableKubernetesResourceAccess grants, mirroring
// charts/tinysystems-operator/templates/manager-rbac.yaml. Read verbs only —
// the flag's name suggests broader power than it has, which is exactly how the
// missing create/delete went unnoticed.
var baseAccess = map[groupResource][]string{
	{"apps", "deployments"}:            {"get", "list", "patch", "update", "watch"},
	{"", "pods"}:                       {"get", "list", "patch", "update", "watch"},
	{"", "services"}:                   {"get", "list", "patch", "update", "watch"},
	{"networking.k8s.io", "ingresses"}: {"get", "list", "patch", "update", "watch"},
}

type groupResource struct {
	group    string
	resource string
}

func (g groupResource) String() string {
	if g.group == "" {
		return g.resource
	}
	return g.group + "/" + g.resource
}

// apiGroups maps a Kubernetes Go package path to its API group. Only the groups
// a component plausibly touches; an unknown package is reported as unmapped
// rather than guessed, so a wrong group never masquerades as a real answer.
var apiGroups = map[string]string{
	"k8s.io/api/core/v1":                   "",
	"k8s.io/api/apps/v1":                   "apps",
	"k8s.io/api/batch/v1":                  "batch",
	"k8s.io/api/networking/v1":             "networking.k8s.io",
	"k8s.io/api/rbac/v1":                   "rbac.authorization.k8s.io",
	"k8s.io/api/policy/v1":                 "policy",
	"k8s.io/api/admissionregistration/v1":  "admissionregistration.k8s.io",
	"k8s.io/api/autoscaling/v1":            "autoscaling",
	"k8s.io/api/autoscaling/v2":            "autoscaling",
	"k8s.io/api/storage/v1":                "storage.k8s.io",
	"k8s.io/api/certificates/v1":           "certificates.k8s.io",
	"k8s.io/api/coordination/v1":           "coordination.k8s.io",
	"k8s.io/api/events/v1":                 "events.k8s.io",
	"k8s.io/apimachinery/pkg/apis/meta/v1": "",
}

// RBACFinding is one call the declaration does not cover.
type RBACFinding struct {
	Resource groupResource
	Verb     string
	Position string
}

// CheckRBACCoverage resolves every Kubernetes client call under dir and returns
// the ones the declared rules do not permit.
//
// Silent on what it cannot see: a call through an interface it cannot resolve,
// an unstructured object whose kind is only known at runtime, or a package
// outside apiGroups produces no finding. Reporting a guess would train authors
// to ignore the output, and the check exists to be believed.
func CheckRBACCoverage(dir string, declared map[groupResource][]string) ([]RBACFinding, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedSyntax | packages.NeedTypes |
			packages.NeedTypesInfo | packages.NeedDeps | packages.NeedImports,
		Dir: dir,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("load packages: %w", err)
	}

	seen := map[string]bool{}
	var findings []RBACFinding

	for _, pkg := range pkgs {
		if pkg.TypesInfo == nil {
			continue
		}
		for _, file := range pkg.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				verbs, ok := clientVerbs[sel.Sel.Name]
				if !ok {
					return true
				}
				if !isK8sClient(pkg.TypesInfo, sel.X) {
					return true
				}
				gr, ok := resourceOf(pkg.TypesInfo, call.Args)
				if !ok {
					return true
				}
				for _, verb := range verbs {
					if permits(declared, gr, verb) {
						continue
					}
					pos := pkg.Fset.Position(call.Pos()).String()
					key := gr.String() + "|" + verb
					if seen[key] {
						continue
					}
					seen[key] = true
					findings = append(findings, RBACFinding{Resource: gr, Verb: verb, Position: pos})
				}
				return true
			})
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Resource.String() != findings[j].Resource.String() {
			return findings[i].Resource.String() < findings[j].Resource.String()
		}
		return findings[i].Verb < findings[j].Verb
	})
	return findings, nil
}

// isK8sClient reports whether the receiver is a controller-runtime client. The
// method names above are common enough that matching on them alone would flag
// every Create in the module.
func isK8sClient(info *types.Info, expr ast.Expr) bool {
	t := info.TypeOf(expr)
	if t == nil {
		return false
	}
	s := t.String()
	return strings.Contains(s, "sigs.k8s.io/controller-runtime/pkg/client.Client") ||
		strings.Contains(s, "sigs.k8s.io/controller-runtime/pkg/client.WithWatch") ||
		strings.Contains(s, "sigs.k8s.io/controller-runtime/pkg/client.Writer") ||
		strings.Contains(s, "sigs.k8s.io/controller-runtime/pkg/client.Reader")
}

// resourceOf finds the Kubernetes object among a call's arguments and maps it
// to its group and resource. The object's position varies — Get takes a key
// first, List takes the list type — so every argument is considered.
func resourceOf(info *types.Info, args []ast.Expr) (groupResource, bool) {
	for _, arg := range args {
		t := info.TypeOf(arg)
		if t == nil {
			continue
		}
		named := namedOf(t)
		if named == nil {
			continue
		}
		obj := named.Obj()
		if obj == nil || obj.Pkg() == nil {
			continue
		}
		group, known := apiGroups[obj.Pkg().Path()]
		if !known {
			continue
		}
		kind := strings.TrimSuffix(obj.Name(), "List")
		if kind == "" {
			continue
		}
		return groupResource{group: group, resource: pluralize(kind)}, true
	}
	return groupResource{}, false
}

func namedOf(t types.Type) *types.Named {
	for {
		switch x := t.(type) {
		case *types.Pointer:
			t = x.Elem()
		case *types.Named:
			return x
		default:
			return nil
		}
	}
}

// pluralize turns a Kind into the resource name the API server uses. Kubernetes
// resource names are the lowercased kind pluralized by ordinary English rules,
// which the two special cases below cover for every kind in apiGroups.
func pluralize(kind string) string {
	lower := strings.ToLower(kind)
	switch {
	case strings.HasSuffix(lower, "s"), strings.HasSuffix(lower, "x"),
		strings.HasSuffix(lower, "ch"), strings.HasSuffix(lower, "sh"):
		return lower + "es" // ingress → ingresses
	case strings.HasSuffix(lower, "y"):
		return strings.TrimSuffix(lower, "y") + "ies" // networkpolicy → networkpolicies
	default:
		return lower + "s"
	}
}

// permits reports whether the declared rules allow verb on gr, honouring the
// wildcards RBAC itself accepts.
func permits(declared map[groupResource][]string, gr groupResource, verb string) bool {
	for _, candidate := range []groupResource{
		gr,
		{group: gr.group, resource: "*"},
		{group: "*", resource: gr.resource},
		{group: "*", resource: "*"},
	} {
		for _, v := range declared[candidate] {
			if v == verb || v == "*" {
				return true
			}
		}
	}
	return false
}

// DeclaredRules flattens the module's requirements into the lookup the check
// needs, folding in what the base access flag grants.
func DeclaredRules(enableBase bool, extra [][3][]string) map[groupResource][]string {
	out := map[groupResource][]string{}
	if enableBase {
		for gr, verbs := range baseAccess {
			out[gr] = append(out[gr], verbs...)
		}
	}
	for _, rule := range extra {
		groups, resources, verbs := rule[0], rule[1], rule[2]
		for _, g := range groups {
			for _, r := range resources {
				gr := groupResource{group: g, resource: r}
				out[gr] = append(out[gr], verbs...)
			}
		}
	}
	return out
}
