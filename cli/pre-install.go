package cli

import (
	"github.com/go-logr/zerologr"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var preInstallCmd = &cobra.Command{
	Use:   "pre-install",
	Short: "pre-install module",
	Run: func(cmd *cobra.Command, args []string) {
		// re-use zerolog
		l := zerologr.New(&log.Logger)
		l.Info("pre-install", "module name", name, "namespace", namespace)
	},
}
