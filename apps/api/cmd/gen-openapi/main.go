package main

import (
	"flag"
	"fmt"
	"os"

	aiHandler "api/internal/platform/ai/handler"
	v2handler "api/internal/platform/apiv2/handler"
	artHandler "api/internal/platform/artifact/handler"
	"api/internal/platform/artifact/service"
	catHandler "api/internal/platform/catalog/handler"
	commHandler "api/internal/platform/community/handler"
	trustHandler "api/internal/platform/trust/handler"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gofiber/fiber/v3"
)

func main() {
	out := flag.String("o", "", "output file (default: stdout)")
	downgrade := flag.Bool("downgrade", false, "emit OpenAPI 3.0.3 instead of 3.1 (for tools without 3.1 support, e.g. oapi-codegen)")
	admin := flag.Bool("admin", false, "emit the oauth-hosted admin API spec (/api/v1/admin/artifact/*) instead of the artifact service spec")
	catalogAdmin := flag.Bool("catalog-admin", false, "emit the catalog admin review-queue spec (/api/v1/admin/catalog/*)")
	catalogV2 := flag.Bool("catalog-v2", false, "emit the NextMoe public API v2 spec (/v2/problems, /v2/vocabularies, …)")
	community := flag.Bool("community", false, "emit the community S2S embed spec (/api/v1/community/*)")
	trust := flag.Bool("trust", false, "emit the trust S2S intake spec (/api/v1/trust/*)")
	trustAdmin := flag.Bool("trust-admin", false, "emit the trust admin review-inbox spec (/api/v1/admin/trust/*)")
	ai := flag.Bool("ai", false, "emit the AI-gateway S2S spec (/api/v1/ai/*)")
	aiAdmin := flag.Bool("ai-admin", false, "emit the AI-gateway usage-dashboard spec (/api/v1/admin/ai/*)")
	flag.Parse()

	app := fiber.New()
	var api huma.API
	switch {
	case *catalogAdmin:
		api = catHandler.SetupAdmin(app, nil, nil, nil, nil)
	case *catalogV2:
		api = v2handler.Setup(app)
	case *community:
		api = commHandler.Setup(app, nil, nil, nil, nil, nil, nil, nil, nil)
	case *trust:
		api = trustHandler.Setup(app, nil, nil, nil, nil, nil)
	case *trustAdmin:
		api = trustHandler.SetupAdmin(app, nil, nil, nil, nil, nil, nil)
	case *ai:
		api = aiHandler.Setup(app, nil)
	case *aiAdmin:
		api = aiHandler.SetupAdmin(app, nil, nil)
	case *admin:
		api = artHandler.SetupAdmin(app, artHandler.NewAdmin(nil, nil, nil))
	default:
		api = artHandler.Setup(app, service.New(nil, nil, nil))
	}

	var (
		b   []byte
		err error
	)
	if *downgrade {
		b, err = api.OpenAPI().DowngradeYAML()
	} else {
		b, err = api.OpenAPI().YAML()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "gen-openapi:", err)
		os.Exit(1)
	}

	if *out == "" {
		_, _ = os.Stdout.Write(b)
		return
	}
	if err := os.WriteFile(*out, b, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "gen-openapi:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *out, len(b))
}
