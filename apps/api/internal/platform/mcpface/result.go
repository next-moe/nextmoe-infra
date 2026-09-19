package mcpface

import (
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func textResult(body []byte) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}
}

func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}
}

func authError() *mcp.CallToolResult {
	return errorResult(
		"Missing or malformed API key. Configure your MCP client to send the header " +
			"`Authorization: Bearer nmk_live_…` on the MCP endpoint. " +
			"Mint a v2 key at " + devPortalURL + ".",
	)
}

func mapUpstream(status int, body []byte) *mcp.CallToolResult {
	if status >= 200 && status < 300 {
		return textResult(body)
	}

	var hint string
	switch status {
	case 401:
		hint = "Authentication failed: the API key was rejected (invalid, expired, or revoked). " +
			"Check the key at " + devPortalURL + "."
	case 403:
		hint = "Forbidden: the API key lacks the scope or tier required for this resource. " +
			"Review the key's scopes at " + devPortalURL + "."
	case 429:
		hint = "Rate limit or daily quota exceeded for this API key. " +
			"Slow down and retry later; check usage at " + devPortalURL + "."
	case 404:
		hint = "Not found: no such record on the NextMoe catalog (it may be unpublished, or r18 while this call omitted nsfw=true)."
	default:
		if status >= 500 {
			hint = fmt.Sprintf("Upstream service error (status %d). Try again later.", status)
		} else {
			hint = fmt.Sprintf("The request was rejected (status %d).", status)
		}
	}
	return errorResult(hint + "\n\nupstream response:\n" + string(body))
}
