package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/rogpeppe/go-internal/semver"
	"github.com/spf13/cobra"
	"github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/schema"
	"github.com/tiny-systems/module/registry"
	"github.com/tiny-systems/module/tools/build"
	api "github.com/tiny-systems/platform-api"
	"net/http"
	"os"
	"strings"
)

var (
	pathToMain string
	targetOs   string
	targetArch string
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "build module",
	Long:  `Run from module's root folder (go.mod && README.md files should exist in a folder) If your main's package path differ from ./cmd please specify path parameter'`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()

		cwd, err := os.Getwd()
		if err != nil {
			fmt.Printf("unable to get current path: %v\n", err)
			os.Exit(1)
			return
		}

		info, err := build.GetReadme(cwd)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "unable to get README.md by path %s: %v\n", cwd, err)
			os.Exit(1)
			return
		}

		platformClient, err := api.NewClientWithResponses(platformApiURL)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "unable to create API client: %v\n", err)
			os.Exit(1)
			return
		}

		componentsApi := make([]api.PublishComponent, 0)
		for _, c := range registry.Get() {
			componentsApi = append(componentsApi, getComponentApi(c))
		}

		if len(componentsApi) == 0 {
			_, _ = fmt.Fprintf(os.Stderr, "component registry is empty\n")
			os.Exit(1)
			return
		}

		if !semver.IsValid(version) {
			_, _ = fmt.Fprintf(os.Stderr, "version is invalid semver v2 version\n")
			os.Exit(1)
			return
		}

		//devKey
		resp, err := platformClient.PublishModuleWithResponse(ctx, api.PublishModuleRequest{
			Name:       name,
			Info:       &info,
			Version:    strings.TrimPrefix(version, "v"),
			Components: componentsApi,
		}, func(ctx context.Context, req *http.Request) error {
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", devKey))
			return nil
		})

		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "unable to publish module: %v\n", err)
			os.Exit(1)
			return
		}
		if resp.JSON200 == nil {
			_, _ = fmt.Fprintf(os.Stderr, "unknown server error: %s\n", string(resp.Body))
			os.Exit(1)
			return
		}

		publishResponse := resp.JSON200
		if publishResponse.Module == nil || publishResponse.Options == nil {
			_, _ = fmt.Fprintf(os.Stderr, "invalid server response\n")
			os.Exit(1)
			return
		}

		buildOpts := build.Options{
			TargetOs:   targetOs,
			TargetArch: targetArch,
			Repo:       publishResponse.Options.Repo,
			Tag:        publishResponse.Options.Tag,
			VersionID:  publishResponse.Module.Id,
		}

		if err := build.Build(ctx, cwd, pathToMain, buildOpts); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "unable to build: %v\n", err)
			os.Exit(1)
			return
		}

		image := fmt.Sprintf("%s:%s", publishResponse.Options.Repo, publishResponse.Options.Tag)

		if err = build.Push(ctx, build.PushOpts{
			Image:      image,
			TargetOs:   targetOs,
			TargetArch: targetArch,
			Username:   publishResponse.Options.Username,
			Password:   publishResponse.Options.Password,
		}); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "unable to push: image %s; error: %v\n", image, err)
			os.Exit(1)
			return
		}
		_, err = platformClient.UpdateModuleVersion(ctx, api.UpdateModuleVersionRequest{
			Id:   publishResponse.Module.Id,
			Repo: publishResponse.Options.Repo,
			Tag:  publishResponse.Options.Tag,
		}, func(ctx context.Context, req *http.Request) error {
			req.Header.Set("Authorization", fmt.Sprintf("Bearer: %s", devKey))
			return nil
		})
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "unable to update version with image %s; error: %v\n", image, err)
			os.Exit(1)
			return
		}

		fmt.Printf("image %s succesfully pushed\n", image)
	},
}

func getComponentApi(c module.Component) api.PublishComponent {
	componentInfo := c.GetInfo()

	// Build ports with schemas
	ports := make([]api.PublishComponentPort, 0)
	for _, p := range c.Ports() {
		port := api.PublishComponentPort{
			Name:   p.Name,
			Source: p.Source,
		}
		if p.Label != "" {
			port.Label = &p.Label
		}
		pos := int(p.Position)
		port.Position = &pos

		// Generate schema from Configuration
		if p.Configuration != nil {
			s, err := schema.CreateSchema(p.Configuration)
			if err == nil {
				schemaBytes, err := s.MarshalJSON()
				if err == nil {
					var schemaMap map[string]interface{}
					if json.Unmarshal(schemaBytes, &schemaMap) == nil {
						port.Schema = &schemaMap
					}
				}
			}
		}
		ports = append(ports, port)
	}

	return api.PublishComponent{
		Name:        componentInfo.Name,
		Description: componentInfo.Description,
		Info:        &componentInfo.Info,
		Tags:        &componentInfo.Tags,
		Ports:       &ports,
	}
}
