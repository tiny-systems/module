package build

import (
	"context"
	"encoding/base64"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/registry"
	"github.com/goccy/go-json"
	"io"
	"os"
)

type PushOpts struct {
	Image      string
	Username   string
	Password   string
	TargetOs   string
	TargetArch string
}

func Push(ctx context.Context, opts PushOpts) error {
	dockerClient, err := getClient()
	if err != nil {
		return err
	}
	defer dockerClient.Close()

	auth := registry.AuthConfig{
		Username: opts.Username,
		Password: opts.Password,
	}
	authBytes, err := json.Marshal(auth)
	if err != nil {
		return err
	}

	// check image locally before push
	summary, err := dockerClient.ImageList(ctx, types.ImageListOptions{
		Filters: filters.NewArgs(filters.Arg("reference", opts.Image)),
	})
	if err != nil {
		return fmt.Errorf("unable to list local images: %w", err)
	}
	if len(summary) == 0 {
		return fmt.Errorf("image %s not found locally", opts.Image)
	}

	imgOpts := types.ImagePushOptions{
		RegistryAuth: base64.URLEncoding.EncodeToString(authBytes),
	}

	if opts.TargetOs != "" && opts.TargetArch != "" {
		imgOpts.Platform = fmt.Sprintf("%s/%s", opts.TargetOs, opts.TargetArch)
	}

	out, err := dockerClient.ImagePush(ctx, opts.Image, imgOpts)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(os.Stdout, out)

	return err
}
