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
	ts := New()

	ts.MCP = append(ts.MCP, MCPToolSpec{
		Tool: &mcp.Tool{
			Name:  "get_s3m_access",
			Title: "Get S3M Access",
			Description: "Explain how to get an S3M token and configure this MCP server. Use if no other S3M tools " +
				"are available but the user is asking for facility operations (submitting a job, starting a " +
				"streaming cluster, etc.)",
			InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`),
		},
		Handler: func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: accessInstructions}},
			}, nil
		},
	})

	return ts
}
