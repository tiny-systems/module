package build

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/docker/docker/api/types"
	"io"
	"os"
)

func Push(ctx context.Context, image string, username string, password string) error {
	dockerClient, err := getClient()
	if err != nil {
		return err
	}
	defer dockerClient.Close()

	auth := types.AuthConfig{
		Username: username,
		Password: password,
	}
	authBytes, err := json.Marshal(auth)
	if err != nil {
		return err
	}

	out, err := dockerClient.ImagePush(ctx, image, types.ImagePushOptions{
		RegistryAuth: base64.URLEncoding.EncodeToString(authBytes),
		Platform:     platform,
	})
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(os.Stdout, out)

	return err
}
