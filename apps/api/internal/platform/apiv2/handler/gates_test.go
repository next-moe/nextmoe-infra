package handler

import (
	"bytes"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

func TestG1SpecIsIdempotent(t *testing.T) {
	a, err := Setup(fiber.New()).OpenAPI().YAML()
	require.NoError(t, err)
	b, err := Setup(fiber.New()).OpenAPI().YAML()
	require.NoError(t, err)
	if !bytes.Equal(a, b) {
		t.Fatalf("spec YAML is not idempotent (%d vs %d bytes)", len(a), len(b))
	}
	require.NotEmpty(t, a)
}

func TestG2toG5(t *testing.T) {
	doc := Setup(fiber.New()).OpenAPI()
	if errs := CheckG1toG5(doc); len(errs) > 0 {
		t.Fatalf("gates:\n  %s", stringsJoin(errs))
	}
}

func TestG6G13(t *testing.T) {
	doc := Setup(fiber.New()).OpenAPI()
	if errs := CheckG6G13(doc); len(errs) > 0 {
		t.Fatalf("gates:\n  %s", stringsJoin(errs))
	}
}

func TestG7toG16(t *testing.T) {
	doc := Setup(fiber.New()).OpenAPI()
	if errs := CheckG7toG16(doc); len(errs) > 0 {
		t.Fatalf("gates:\n  %s", stringsJoin(errs))
	}
}

func TestG10G15G17(t *testing.T) {
	doc := Setup(fiber.New()).OpenAPI()
	var errs []string
	errs = append(errs, CheckG10(doc)...)
	errs = append(errs, CheckG15(doc)...)
	errs = append(errs, CheckG17(doc)...)
	errs = append(errs, CheckG18(doc)...)
	if len(errs) > 0 {
		t.Fatalf("gates:\n  %s", stringsJoin(errs))
	}
}

func stringsJoin(errs []string) string {
	out := errs[0]
	for i := 1; i < len(errs); i++ {
		out += "\n  " + errs[i]
	}
	return out
}
