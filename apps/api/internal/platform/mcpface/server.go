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
	"No key is needed: catalog, news, problems, vocabularies, stats and schemas all answer anonymously, " +
	"with one small rate limit and daily quota per IP address. Send `Authorization: Bearer nmk_live_…` on the MCP endpoint " +
	"for your own, higher limit; any application mints its own v2 key at " + devPortalURL + ", no approval required. " +
	"listCatalogClaimEvents is the one exception: it needs a key holding the operator-granted claim_events:read scope. " +
	"v1 nm_live_ keys are not accepted. " +
	"R18 content is hidden by default: pass nsfw=true to include it. Any caller may do so. " +
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
