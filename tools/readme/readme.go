package readme

import (
	"os"
	"path"
	"strings"
)

func GetReadme(p string) (string, error) {
	data, err := os.ReadFile(path.Join(p, "README.md"))
	if err != nil {
		return "", err
	}
	b := strings.Builder{}
	b.Write(data)
	return b.String(), err
}
