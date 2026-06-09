package toolset

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/olcf/s3m-cli/internal/proto"
)

type MCPToolSpec struct {
	Tool    *mcp.Tool
	Handler mcp.ToolHandler
}

type HTTPRouteSpec struct {
	Path                string
	Handler             http.HandlerFunc
	Method              proto.MethodInfo
	RequiredMethodNames []string
	RequiresPermission  bool
}

type OpenAPIPathSpec struct {
	Path     string
	PathItem map[string]any
}

type ToolSet struct {
	MCP     []MCPToolSpec
	HTTP    []HTTPRouteSpec
	OpenAPI []OpenAPIPathSpec
}

func New() *ToolSet {
	return &ToolSet{
		MCP:     make([]MCPToolSpec, 0),
		HTTP:    make([]HTTPRouteSpec, 0),
		OpenAPI: make([]OpenAPIPathSpec, 0),
	}
}

func (ts *ToolSet) Merge(other *ToolSet) {
	if other == nil {
		return
	}

	ts.MCP = append(ts.MCP, other.MCP...)
	ts.HTTP = append(ts.HTTP, other.HTTP...)
	ts.OpenAPI = append(ts.OpenAPI, other.OpenAPI...)
}
