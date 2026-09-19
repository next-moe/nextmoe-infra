package mcpface

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	serverName    = "nextmoe-catalog"
	serverVersion = "0.1.0-m1"
)

const instructions = "NextMoe catalog v2: read-only tools generated from the public OpenAPI document. " +
	"Each tool is one GET on /v2. Tool names are OpenAPI operationIds. Parameters match the HTTP " +
	"query and path parameters, including view= and fields=. " +
	"Send `Authorization: Bearer nmk_live_…` on the MCP endpoint for catalog reads that require a key; " +
	"any application mints its own v2 key at " + devPortalURL + ", no approval required. " +
	"v1 nm_live_ keys are not accepted. News, problems, vocabularies, stats and schemas need no key. " +
	"R18 content is hidden by default: pass nsfw=true to include it. Any key may do so. " +
	"The v2 contract is stable and changes only additively; this tool set follows the published document."

func NewServer(up *Upstream, spec []byte) (*mcp.Server, *SpecSync, error) {
	s := mcp.NewServer(&mcp.Implementation{Name: serverName, Version: serverVersion},
		&mcp.ServerOptions{Instructions: instructions})
	sync := &SpecSync{srv: s, up: up}
	if _, err := sync.Apply(spec); err != nil {
		return nil, nil, err
	}
	return s, sync, nil
}
