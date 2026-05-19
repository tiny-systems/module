package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/go-logr/zerologr"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

// reservedNamespaces are kubernetes-built-in namespaces that must
// never be used for tinysystems module installs. Mixing module
// workloads with kube-system or default brings every misconfig in
// the cluster into the blast radius of any module that opts into
// secret reads or RBAC ExtraRules.
var reservedNamespaces = map[string]bool{
	"default":          true,
	"kube-system":      true,
	"kube-public":      true,
	"kube-node-lease":  true,
}

// managedLabelKey is the namespace label that opts a namespace into
// hosting tinysystems modules. Users set it manually (kubectl label
// namespace <ns> tinysystems.io/managed=true) — explicit consent,
// no implicit auto-labeling that could surprise the operator.
const managedLabelKey = "tinysystems.io/managed"

var preInstallCmd = &cobra.Command{
	Use:   "pre-install",
	Short: "pre-install module — validate the install namespace before any chart resources land",
	Run: func(cmd *cobra.Command, args []string) {
		l := zerologr.New(&log.Logger)
		ns := os.Getenv("RELEASE_NAMESPACE")
		if ns == "" {
			// Fall back to the legacy --namespace flag so this still
			// behaves on chart versions that haven't yet wired the
			// downward-API env var into the hook Job pod spec.
			ns = namespace
		}
		l.Info("pre-install: validating install namespace", "namespace", ns, "module", name)

		if err := validateNamespace(cmd.Context(), ns); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
		l.Info("pre-install: namespace OK", "namespace", ns)
	},
}

func validateNamespace(ctx context.Context, ns string) error {
	if ns == "" {
		return fmt.Errorf("pre-install: RELEASE_NAMESPACE is empty — chart Job spec must inject it via the downward API")
	}
	if reservedNamespaces[ns] {
		return fmt.Errorf(
			"pre-install: refusing to install into reserved namespace %q. "+
				"Create a dedicated namespace:\n"+
				"  kubectl create namespace tinysystems\n"+
				"  kubectl label namespace tinysystems %s=true",
			ns, managedLabelKey,
		)
	}

	cfg, err := config.GetConfig()
	if err != nil {
		return fmt.Errorf("pre-install: failed to load in-cluster config: %w", err)
	}
	c, err := client.New(cfg, client.Options{})
	if err != nil {
		return fmt.Errorf("pre-install: failed to create k8s client: %w", err)
	}

	var nsObj corev1.Namespace
	if err := c.Get(ctx, types.NamespacedName{Name: ns}, &nsObj); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("pre-install: namespace %q not found — should have been created by helm --create-namespace", ns)
		}
		if apierrors.IsForbidden(err) {
			return fmt.Errorf(
				"pre-install: pod ServiceAccount lacks 'get' on namespaces — chart must grant a ClusterRole with resourceNames=[%q]. Underlying error: %w",
				ns, err,
			)
		}
		return fmt.Errorf("pre-install: get namespace %s: %w", ns, err)
	}

	if nsObj.Labels[managedLabelKey] != "true" {
		return fmt.Errorf(
			"pre-install: namespace %q is not labeled for tinysystems. "+
				"Either label this namespace to dedicate it:\n"+
				"  kubectl label namespace %s %s=true\n"+
				"or pick a different namespace already labeled. "+
				"This prevents accidentally installing modules into namespaces shared with unrelated workloads.",
			ns, ns, managedLabelKey,
		)
	}
	return nil
}
