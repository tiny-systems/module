package cli

import (
	"context"
	"fmt"
	"github.com/rogpeppe/go-internal/semver"
	"github.com/spf13/cobra"
	"github.com/tiny-systems/module/module"
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
			return
		}

		info, err := build.GetReadme(cwd)
		if err != nil {
			fmt.Printf("unable to get README.md by path %s: %v\n", cwd, err)
			return
		}

		platformClient, err := api.NewClientWithResponses(platformApiURL)
		if err != nil {
			fmt.Printf("unable to create API client: %v\n", err)
			return
		}

		componentsApi := make([]api.PublishComponent, 0)
		for _, c := range registry.Get() {
			componentsApi = append(componentsApi, getComponentApi(c))
		}

		if len(componentsApi) == 0 {
			fmt.Printf("component registry is empty\n")
			return
		}

		if !semver.IsValid(version) {
			fmt.Printf("version is invalid semver v2 version\n")
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
			fmt.Printf("unable to publish module: %v\n", err)
			return
		}
		if resp.JSON200 == nil {
			fmt.Printf("unknown server error: %s\n", string(resp.Body))
			return
		}

		publishResponse := resp.JSON200
		if publishResponse.Module == nil || publishResponse.Options == nil {
			fmt.Printf("invalid server response\n")
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
			fmt.Printf("unable to build: %v\n", err)
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
			fmt.Printf("unable to push: image %s; error: %v\n", image, err)
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
			fmt.Printf("unable to update version with image %s; error: %v\n", image, err)
			return
		}
		fmt.Printf("image %s succesfully pushed\n", image)
	},
}

func getComponentApi(c module.Component) api.PublishComponent {
	componentInfo := c.GetInfo()
	return api.PublishComponent{
		Name:        componentInfo.Name,
		Description: componentInfo.Description,
		Info:        &componentInfo.Info,
		Tags:        &componentInfo.Tags,
	}
}
