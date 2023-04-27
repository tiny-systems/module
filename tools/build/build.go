package build

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"golang.org/x/mod/modfile"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	goModFile  = "go.mod"
	replaceDir = "__replaces"
	platform   = "linux/arm64"
)

var (
	//go:embed Dockerfile
	dockerfile string
)

func Build(ctx context.Context, cwd string, pathToMain string, bOpts Options) error {

	_, err := os.Stat(pathToMain)
	if err != nil {
		return errors.Wrap(err, "unable to locate main package")
	}

	goModData, err := os.ReadFile(path.Join(cwd, goModFile))
	if err != nil {
		return errors.Wrap(err, "unable to find go.mod file")
	}

	goMod, err := modfile.Parse("go.mod", goModData, nil)
	if err != nil {
		return err
	}

	dockerClient, err := getClient()
	if err != nil {
		return err
	}
	defer dockerClient.Close()

	u, err := uuid.NewUUID()
	if err != nil {
		return err
	}

	var prepare []string

	var buf bytes.Buffer
	tarWriter := tar.NewWriter(&buf)

	//write tar files
	if err = addTar(cwd, "", tarWriter); err != nil {
		return err
	}

	for _, r := range goMod.Replace {
		// copy to context
		if r.New.Version != "" {
			// skip non local replaces
			continue
		}
		replaceTarPath := fmt.Sprintf("%s/%s", replaceDir, path.Base(r.New.Path))
		if err = addTar(r.New.Path, replaceTarPath, tarWriter); err != nil {
			return err
		}
		prepare = append(prepare, fmt.Sprintf("RUN go mod edit -replace %s=%s", r.Old.Path, fmt.Sprintf("./%s", replaceTarPath)))
	}

	dockerfile =
		strings.ReplaceAll(
			strings.ReplaceAll(
				strings.ReplaceAll(
					strings.ReplaceAll(dockerfile,
						"%VERSION_ID%", bOpts.VersionID),
					"%MAIN_PATH%", pathToMain),
				"%MOD_PREPARE%", strings.Join(prepare, "\n")),
			"%PLATFORM%", platform,
		)

	dockerFileName := fmt.Sprintf("Dockerfile.%s", u.String())

	if err = appendFile(dockerFileName, []byte(dockerfile), tarWriter); err != nil {
		return err
	}
	if err := tarWriter.Close(); err != nil {
		return err
	}

	imgOpts := types.ImageBuildOptions{
		Dockerfile: dockerFileName,
		Remove:     true,
		Platform:   platform,
		Tags:       []string{fmt.Sprintf("%s:%s", bOpts.Repo, bOpts.Tag)},
	}

	res, err := dockerClient.ImageBuild(ctx, bytes.NewReader(buf.Bytes()), imgOpts)
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(res.Body)
	for scanner.Scan() {
		fmt.Println(scanner.Text())
	}
	return nil
}

func appendFile(filename string, data []byte, tw *tar.Writer) error {
	hdr := &tar.Header{
		Name: filename,
		Mode: 0600,
		Size: int64(len(data)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := tw.Write(data); err != nil {
		return err
	}
	return nil
}

func addTar(src string, dst string, tw *tar.Writer) error {

	// ensure the src actually exists before trying to tar it
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("unable to tar files - %v", err.Error())
	}
	// walk path
	return filepath.Walk(src, func(file string, fi os.FileInfo, err error) error {
		// return on any error
		if err != nil {
			return err
		}
		// return on non-regular files (thanks to [kumo](https://medium.com/@komuw/just-like-you-did-fbdd7df829d3) for this suggested update)
		if !fi.Mode().IsRegular() {
			return nil
		}
		// create a new dir/file header
		header, err := tar.FileInfoHeader(fi, fi.Name())
		if err != nil {
			return err
		}
		// update the name to correctly reflect the desired destination when untaring
		header.Name = strings.TrimPrefix(path.Join(dst, strings.Replace(file, src, "", -1)), string(filepath.Separator))

		// write the header
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		// open files for taring
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		// copy file data into tar writer
		if _, err := io.Copy(tw, f); err != nil {
			return err
		}
		// manually close here after each file operation; defering would cause each file close
		// to wait until all operations have completed.
		f.Close()
		return nil
	})
}

type Options struct {
	Repo      string
	Tag       string
	VersionID string
}

func getClient() (*client.Client, error) {
	client, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return client, err
}
