package cli

import "github.com/spf13/cobra"

var (
	serverKey                 string
	grpcServerInsecureConnect bool
	grpcConnStr               string
)

func RegisterCommands(root *cobra.Command) {
	runCmd.Flags().StringVarP(&serverKey, "key", "k", "", "Server key")
	applyCommonBuildFlags(runCmd)

	applyConnFlags(runCmd)
	root.AddCommand(runCmd)
	//
	root.AddCommand(infoCmd)
	//
	buildCmd.Flags().StringVarP(&pathToMain, "path", "p", "./cmd", "path to main package regarding to the root")
	applyCommonBuildFlags(buildCmd)
	//
	buildCmd.Flags().StringVarP(&devKey, "devkey", "d", "", "developer key")
	buildCmd.MarkFlagRequired("devkey")

	applyConnFlags(buildCmd)
	root.AddCommand(buildCmd)
}

func applyConnFlags(cmd *cobra.Command) {
	cmd.Flags().BoolVarP(&grpcServerInsecureConnect, "insecure", "i", false, "gRPC connect with insecure credentials")
	cmd.Flags().StringVarP(&grpcConnStr, "conn", "c", "localhost:50189", "gRPC server connection string")
}

func applyCommonBuildFlags(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&version, "version", "v", "0.0.0", "version")
	cmd.Flags().StringVarP(&name, "name", "n", "main", "module name")
}
