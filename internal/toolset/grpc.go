package toolset

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	grpcclient "github.com/olcf/s3m-cli/internal/grpc"
	"github.com/olcf/s3m-cli/internal/proto"
)

//
// Tool building

func BuildGRPCToolSet(
	methods []proto.MethodInfo, conn *grpc.ClientConn, callTimeout time.Duration, debug bool,
) *ToolSet {
	toolSet := New()

	toolNames := make([]string, 0, len(methods))

	for _, m := range methods {
		inputSchema := proto.SchemaForMethod(m.Method, m.Headers)
		outputSchema := proto.SchemaForMessage(m.Method.Output())

		schemaJSON, err := json.Marshal(inputSchema)
		if err != nil {
			slog.Warn("failed to marshal input schema", "tool", m.ToolName, "error", err)

			schemaJSON, err = json.Marshal(map[string]any{"type": "object"})
			if err != nil {
				slog.Error("failed to marshal fallback schema", "error", err)

				continue
			}
		}

		mk := grpcclient.MethodKey{
			ServiceFull: string(m.Service.FullName()),
			Method:      string(m.Method.Name()),
		}

		toolSet.MCP = append(toolSet.MCP, MCPToolSpec{
			Tool: &mcp.Tool{
				Name:        m.ToolName,
				Title:       string(m.Method.FullName()),
				Description: m.Desc,
				InputSchema: json.RawMessage(schemaJSON),
			},
			Handler: makeHandler(mk, m.Method, conn, m.Headers, callTimeout, debug),
		})

		toolSet.HTTP = append(toolSet.HTTP, HTTPRouteSpec{
			Path:               m.Path,
			Handler:            makeOpenAPIHandler(mk, m.Method, conn, m.Headers, callTimeout, debug),
			Method:             m,
			RequiresPermission: true,
		})

		toolSet.OpenAPI = append(toolSet.OpenAPI, OpenAPIPathSpec{
			Path:     m.Path,
			PathItem: buildOpenAPIPathItem(m.Desc, m.ToolName, inputSchema, outputSchema),
		})

		toolNames = append(toolNames, m.ToolName)
	}

	if debug && len(toolNames) > 0 {
		slog.Info("gRPC tools prepared", "count", len(toolNames))
	}

	return toolSet
}

//
// Handlers

func makeHandler(
	key grpcclient.MethodKey,
	md protoreflect.MethodDescriptor,
	conn *grpc.ClientConn,
	headers []proto.HeaderParam,
	callTimeout time.Duration,
	debug bool,
) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		argsJSON := req.Params.Arguments

		bodyJSON, headerVals, err := proto.ExtractHeadersAndBody(argsJSON, headers)
		if err != nil {
			return ErrorResult(err), nil
		}

		if conn == nil {
			inMsg := dynamicpb.NewMessage(md.Input())

			if len(bodyJSON) > 0 {
				if err := protojson.Unmarshal(bodyJSON, inMsg); err != nil {
					return ErrorResult(fmt.Errorf("invalid arguments for %s/%s: %w", key.ServiceFull, key.Method, err)), nil
				}
			}

			js, _ := protojson.Marshal(inMsg)

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: fmt.Sprintf(
						"dry-run %s/%s (no grpc_target configured)\nrequest JSON:\n%s",
						key.ServiceFull, key.Method, string(js),
					)},
				},
			}, nil
		}

		respJSON, err := grpcclient.InvokeJSON(ctx, bodyJSON, md, conn, key, headerVals, callTimeout, debug)
		if err != nil {
			return ErrorResult(err), nil
		}

		structured, err := wrapStructuredContent(respJSON)
		if err != nil {
			return ErrorResult(err), nil
		}

		return &mcp.CallToolResult{
			Content:           []mcp.Content{&mcp.TextContent{Text: string(respJSON)}},
			StructuredContent: structured,
		}, nil
	}
}

//
// Utilities

func ErrorResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		IsError: true,
	}
}

func wrapStructuredContent(respJSON []byte) (any, error) {
	var structured any
	if err := json.Unmarshal(respJSON, &structured); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if _, isObj := structured.(map[string]any); !isObj {
		structured = map[string]any{"value": structured}
	}

	return structured, nil
}

func buildOpenAPIPathItem(desc string, toolName string, inputSchema, outputSchema map[string]any) map[string]any {
	return map[string]any{
		"post": map[string]any{
			"summary":     desc,
			"operationId": toolName,
			"requestBody": map[string]any{
				"required": true,
				"content": map[string]any{
					"application/json": map[string]any{
						"schema": inputSchema,
					},
				},
			},
			"responses": map[string]any{
				"200": map[string]any{
					"description": "OK",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": outputSchema,
						},
					},
				},
			},
		},
	}
}
