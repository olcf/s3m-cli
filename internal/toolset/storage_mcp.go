package toolset

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//
// MCP handlers

func buildStorageMCP[T any, R any](fn func(context.Context, T) (R, error)) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var input T

		if len(req.Params.Arguments) > 0 {
			if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
				return ErrorResult(fmt.Errorf("parse arguments: %w", err)), nil
			}
		}

		resp, err := fn(ctx, input)
		if err != nil {
			return ErrorResult(err), nil
		}

		return structuredResult(resp)
	}
}

func (h *storageHandlers) listDatasetsMCP(
	ctx context.Context, req *mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	return buildStorageMCP(h.handleListDatasets)(ctx, req)
}

func (h *storageHandlers) listFilesMCP(
	ctx context.Context, req *mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	return buildStorageMCP(h.handleListFiles)(ctx, req)
}

func (h *storageHandlers) readFileMCP(
	ctx context.Context, req *mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	return buildStorageMCP(h.handleReadFile)(ctx, req)
}

func (h *storageHandlers) getDownloadURLMCP(
	ctx context.Context, req *mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	return buildStorageMCP(h.handleGetDownloadURL)(ctx, req)
}

func (h *storageHandlers) putFileMCP(
	ctx context.Context, req *mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	return buildStorageMCP(h.handlePutFile)(ctx, req)
}

func (h *storageHandlers) deleteDatasetMCP(
	ctx context.Context, req *mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	return buildStorageMCP(h.handleDeleteDataset)(ctx, req)
}
