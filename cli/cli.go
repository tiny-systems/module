package cli

import (
	"github.com/spf13/cobra"
	"k8s.io/client-go/util/homedir"
	"path/filepath"
)

func RegisterCommands(root *cobra.Command) {
	applyRunFlags(runCmd)
	root.AddCommand(runCmd)
}

func applyRunFlags(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&name, "name", "n", "main", "Name of the module. Container image repo usually.")
	cmd.Flags().StringVarP(&kubeconfig, "kubeconfig", "k", filepath.Join(homedir.HomeDir(), ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	cmd.Flags().StringVarP(&metricsAddr, "metrics-bind-address", "m", ":0", "The address the metric endpoint binds to.")
	cmd.Flags().StringVarP(&probeAddr, "health-probe-bind-address", "t", ":0", "The address the probe endpoint binds to.")
	cmd.Flags().StringVarP(&grpcAddr, "grpc-server", "g", ":0", "gRPC server listen address")
	cmd.Flags().BoolVarP(&enableLeaderElection, "leader-elect", "l", false, "Enable leader election for controller manager. "+
		"Enabling this will ensure there is only one active controller manager.")
}
