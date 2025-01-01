package cli

import (
	"github.com/spf13/cobra"
	"k8s.io/client-go/util/homedir"
	"path/filepath"
	"runtime"
)

var (
	platformApiURL string
)

func RegisterCommands(rootCmd *cobra.Command) {

	applyCommonFlags(rootCmd)

	applyRunFlags(runCmd)
	rootCmd.AddCommand(runCmd)

	applyToolsFlags(toolsCmd)
	rootCmd.AddCommand(toolsCmd)

	applyBuildFlags(buildCmd)
	toolsCmd.AddCommand(buildCmd)
	toolsCmd.AddCommand(infoCmd)

	applyHookFlags(preInstallCmd)
	rootCmd.AddCommand(preInstallCmd)
	applyHookFlags(preDeleteCmd)
	rootCmd.AddCommand(preDeleteCmd)
}

func applyCommonFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVarP(&platformApiURL, "platform-api-url", "", "https://api.tinysystems.io", "Platform API URL")
}

func applyToolsFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVarP(&devKey, "devkey", "d", "", "developer key")
}

func applyBuildFlags(cmd *cobra.Command) {

	cmd.Flags().StringVarP(&pathToMain, "path", "p", "./cmd", "path to main package regarding to the root")
	cmd.Flags().StringVarP(&version, "version", "v", "", "module version")
	cmd.Flags().StringVarP(&name, "name", "n", "main", "Name of the module. Container image repo usually.")
	cmd.Flags().StringVarP(&targetOs, "os", "o", "linux", "Target OS, change only if you are using custom Dockerfile with OS different than linux.")
	cmd.Flags().StringVarP(&targetArch, "arch", "a", runtime.GOARCH, "Target architecture")

	_ = cmd.MarkFlagRequired("devkey")
	_ = cmd.MarkFlagRequired("version")
	_ = cmd.MarkFlagRequired("name")
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
	cmd.Flags().BoolVarP(&enableLeaderElection, "leader-elect", "l", false, "Enable leader election for controller manager. "+
		"Enabling this will ensure there is only one active controller manager.")

	_ = cmd.MarkFlagRequired("version")
	_ = cmd.MarkFlagRequired("name")
}
