package cli

import (
	"context"
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/tiny-systems/module"
	tinyserver "github.com/tiny-systems/module/pkg/api/module-go"
	"github.com/tiny-systems/module/pkg/discovery"
	m "github.com/tiny-systems/module/pkg/module"
	"github.com/tiny-systems/module/platform"
	"github.com/tiny-systems/module/registry"
	"sync"
)

// override by ldflags
var (
	version   string
	name      string
	versionID string // ldflags
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "run module",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {

		log.Info().Str("versionID", versionID).Msg("starting...")
		// run all modules
		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()
		if serverKey == "" {
			serverKey = viper.GetString("server_key")
		}
		if serverKey == "" {
			log.Fatal().Msg("no server key defined")
		}
		if natsConnStr == "" {
			natsConnStr = nats.DefaultURL
		}
		nc, err := nats.Connect(natsConnStr)
		if err != nil {
			log.Fatal().Err(err).Msg("unable to connect to NATS")
		}
		defer nc.Close()

		manifestResp, err := platform.NewClient(nc).GetManifest(ctx, &tinyserver.GetManifestRequest{
			ServerKey:       serverKey,
			ModuleVersionID: versionID,
			Version:         version,
		})
		if err != nil {
			log.Fatal().Err(err).Msg("manifest error")
		}

		errChan := make(chan error)
		defer close(errChan)
		go func() {
			for err := range errChan {
				log.Error().Err(err).Msg("")
			}
		}()

		r := discovery.NewRegistry(nc)

		serv := module.New(manifestResp.RunnerConfig, errChan)
		serv.SetLogger(log.Logger)
		serv.SetNats(nc)
		serv.SetRegistry(r)

		wg := &sync.WaitGroup{}
		wg.Add(1)

		go func() {
			defer wg.Done()
			if err := serv.Run(ctx); err != nil {
				log.Error().Err(err).Msg("unable to run server")
			}
		}()

		info := m.Info{
			Version:   version,
			Name:      name,
			VersionID: versionID,
		}

		for _, cmp := range registry.Get() {
			if err = serv.InstallComponent(ctx, info, cmp); err != nil {
				log.Error().Err(err).Str("component", cmp.GetInfo().Name).Msg("unable to install component")
			}
		}
		for _, instance := range manifestResp.Instances {
			serv.RunInstance(instance)
		}
		wg.Wait()
		log.Info().Msg("all done")
	},
}
