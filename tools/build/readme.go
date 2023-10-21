package build

import (
	"os"
	"path"
	"strings"
)

const (
	ReadmeFile = "README.md"
)

func GetReadme(p string) (string, error) {
	data, err := os.ReadFile(path.Join(p, ReadmeFile))
	if err != nil {
		return "", err
	}
	b := strings.Builder{}
	b.Write(data)
	return b.String(), err
}
