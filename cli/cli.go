package cli

import (
	"github.com/spf13/cobra"
	"k8s.io/client-go/util/homedir"
	"path/filepath"
)

func RegisterCommands(rootCmd *cobra.Command) {

	// No --platform-api-url or --devkey: both existed only to authenticate the
	// `publish` command against the platform's /v1/devtools endpoints, which are
	// retired. A module is published by its own CI and discovered from a repo's
	// index.yaml, so a module binary never calls the platform API.

	applyRunFlags(runCmd)
	rootCmd.AddCommand(runCmd)

	rootCmd.AddCommand(toolsCmd)

	infoCmd.Flags().BoolVar(&componentsInfoJSON, "json", false, "emit the discovery shape for a repo index's components.yaml")
	toolsCmd.AddCommand(infoCmd)
	toolsCmd.AddCommand(rbacValuesCmd)
	rbacCheckCmd.Flags().BoolVar(&rbacCheckStrict, "strict", false, "exit non-zero when a call is not covered")
	toolsCmd.AddCommand(rbacCheckCmd)

	applyHookFlags(preInstallCmd)
	rootCmd.AddCommand(preInstallCmd)
	applyHookFlags(preDeleteCmd)
	rootCmd.AddCommand(preDeleteCmd)
}

func applyHookFlags(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&name, "name", "n", "main", "Name of the module. Container image repo usually.")
	cmd.Flags().StringVarP(&namespace, "namespace", "", "tinysystems", "Namespace where the module installed in.")
	cmd.Flags().StringVarP(&kubeconfig, "kubeconfig", "k", filepath.Join(homedir.HomeDir(), ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	_ = cmd.MarkFlagRequired("name")
}

func applyRunFlags(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&version, "version", "v", "", "module version")
	cmd.Flags().StringVarP(&name, "name", "n", "main", "Name of the module. Container image repo usually.")
	cmd.Flags().StringVarP(&namespace, "namespace", "", "tinysystems", "Namespace where the module installed in.")
	cmd.Flags().StringVarP(&kubeconfig, "kubeconfig", "k", filepath.Join(homedir.HomeDir(), ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	cmd.Flags().StringVarP(&metricsAddr, "metrics-bind-address", "m", ":0", "The address the metric endpoint binds to.")
	cmd.Flags().StringVarP(&probeAddr, "health-probe-bind-address", "t", ":0", "The address the probe endpoint binds to.")
	cmd.Flags().StringVarP(&grpcAddr, "grpc-server-bind-address", "g", ":0", "The address the gRPC server binds to.")

	_ = cmd.MarkFlagRequired("version")
	_ = cmd.MarkFlagRequired("name")
}
