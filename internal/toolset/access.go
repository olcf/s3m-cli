package toolset

import (
	"context"
	_ "embed"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed access_instructions.txt
var accessInstructions string

func BuildUnauthenticatedToolSet() *ToolSet {
	return buildAccessToolSet(
		"Get S3M Access",
		"Explain how to get an S3M token and configure this MCP server. Use if no other S3M tools "+
			"are available but the user is asking for facility operations (submitting a job, starting a "+
			"streaming cluster, etc.)",
		accessInstructions,
	)
}

func BuildAuthenticationErrorToolSet() *ToolSet {
	return buildAccessToolSet(
		"Resolve S3M Authentication",
		"Explain that the supplied S3M token could not be validated and how to recover.",
		"The supplied S3M token could not be validated. The authentication service may be temporarily "+
			"unavailable, or the token may be invalid or expired. Retry the request. If it continues to fail, "+
			"create a new token in myOLCF and use it for this MCP server. Do not paste the token into this "+
			"conversation.",
	)
}

func buildAccessToolSet(title, description, instructions string) *ToolSet {
	ts := New()

	ts.MCP = append(ts.MCP, MCPToolSpec{
		Tool: &mcp.Tool{
			Name:        "get_s3m_access",
			Title:       title,
			Description: description,
			InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`),
		},
		Handler: func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: instructions}},
			}, nil
		},
	})

	return ts
}
