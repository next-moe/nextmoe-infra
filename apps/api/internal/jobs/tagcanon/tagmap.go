package tagcanon

import (
	"os"

	"api/internal/platform/catalog/vndbtagmap"
)

func ParseTagMap(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return vndbtagmap.Parse(data), nil
}
