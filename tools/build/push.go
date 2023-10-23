package build

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/registry"
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
