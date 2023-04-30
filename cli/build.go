package cli

import (
	"fmt"
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	tinymodule "github.com/tiny-systems/module/pkg/api/module-go"
	"github.com/tiny-systems/module/pkg/utils"
	"github.com/tiny-systems/module/platform"
	"github.com/tiny-systems/module/registry"
	"github.com/tiny-systems/module/tools/build"
	"github.com/tiny-systems/module/tools/readme"
	"os"
)

var (
	devKey     string
	pathToMain string
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "build module",
	Long:  `Run from module's root folder (go.mod && README.md files should exist there) If your main's package path differ from ./cmd please specify path parameter'`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()

		cwd, err := os.Getwd()
		if err != nil {
			log.Fatal().Err(err).Msgf("unable to get current path: %v", err)
		}

		info, err := readme.GetReadme(cwd)
		if err != nil {
			log.Fatal().Err(err).Msgf("unable to get README.md by path %s: %v", cwd, err)
		}

		if natsConnStr == "" {
			natsConnStr = nats.DefaultURL
		}
		nc, err := nats.Connect(natsConnStr)
		if err != nil {
			log.Fatal().Err(err).Msg("unable to connect to NATS")
		}
		defer nc.Close()

		platformClient := platform.NewClient(nc)

		componentsApi := make([]*tinymodule.Component, 0)
		for _, c := range registry.Get() {
			cmpApi, err := utils.GetComponentApi(c)
			if err != nil {
				log.Error().Err(err).Msg("component to api")
				continue
			}
			componentsApi = append(componentsApi, cmpApi)
		}

		resp, err := platformClient.PublishModule(ctx, &tinymodule.PublishModuleRequest{
			Name:         name,
			Info:         info,
			Version:      version,
			DeveloperKey: devKey,
			Components:   componentsApi,
		})
		if err != nil {
			log.Fatal().Err(err).Msg("unable to publish module")
		}
		if resp.Module == nil {
			log.Fatal().Err(err).Msg("invalid server response")
		}

		buildOpts := build.Options{
			Repo:      resp.Options.Repo,
			Tag:       resp.Options.Tag,
			VersionID: resp.Module.ID,
		}
		if err := build.Build(ctx, cwd, pathToMain, buildOpts); err != nil {
			log.Fatal().Err(err).Msg("unable to build")
		}
		image := fmt.Sprintf("%s:%s", resp.Options.Repo, resp.Options.Tag)

		if err = build.Push(ctx, image, resp.Options.Username, resp.Options.Password); err != nil {
			log.Fatal().Err(err).Str("image", image).Msg("unable to push")
		}

		_, err = platformClient.UpdateModuleVersion(ctx, &tinymodule.UpdateModuleVersionRequest{
			ID:   resp.Module.ID,
			Repo: resp.Options.Repo,
			Tag:  resp.Options.Tag,
		})
		if err != nil {
			log.Fatal().Err(err).Str("image", image).Msg("unable to update server")
		}
		log.Info().Str("image", image).Msg("pushed")
	},
}
