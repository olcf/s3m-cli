package toolset

import (
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc"

	"github.com/olcf/s3m-cli/internal/docs"
	"github.com/olcf/s3m-cli/internal/proto"
)

//
// Tool building

const storageBucketGatewayService = "olcf.s3m.storage.v1alpha.BucketGateway"

//nolint:funlen // Tool registration is inherently verbose.
func BuildStorageToolSet(
	conn *grpc.ClientConn, timeout time.Duration, debug bool, allowed []proto.MethodInfo,
) *ToolSet {
	if conn == nil {
		return nil
	}

	allowedMethods := storageAllowedMethodNames(allowed)
	if len(allowedMethods) == 0 {
		return nil
	}

	h := &storageHandlers{
		conn:    conn,
		timeout: timeout,
		debug:   debug,
	}
	ts := New()

	appendStorageTool(ts, allowedMethods, storageToolRegistration{
		requiredMethods: []string{"ListDatasets"},
		name:            "storage_list_datasets",
		title:           "List Datasets",
		description:     "List all datasets in the current project.",
		schema:          listDatasetsSchema,
		mcpHandler:      h.listDatasetsMCP,
		path:            "/storage/list_datasets",
		httpHandler:     h.listDatasetsHTTP,
		summary:         "List datasets",
	})

	appendStorageTool(ts, allowedMethods, storageToolRegistration{
		requiredMethods: []string{"GetDatasetContents"},
		name:            "storage_list_files",
		title:           "List Files",
		description:     "List files in a dataset with optional glob/prefix filtering.",
		schema:          listFilesSchema,
		mcpHandler:      h.listFilesMCP,
		path:            "/storage/list_files",
		httpHandler:     h.listFilesHTTP,
		summary:         "List files in a dataset",
	})

	if hasStorageMethods(allowedMethods, "GetDownloadURLs") {
		ts.MCP = append(ts.MCP,
			MCPToolSpec{
				Tool: &mcp.Tool{
					Name:        "storage_read_file",
					Title:       "Read File",
					Description: "Read file content from a dataset with offset/length support.",
					InputSchema: mustMarshalSchema(readFileSchema),
				},
				Handler: h.readFileMCP,
			},
			MCPToolSpec{
				Tool: &mcp.Tool{
					Name:        "storage_get_download_url",
					Title:       "Get Download URL",
					Description: "Get a presigned download URL for a file in a dataset.",
					InputSchema: mustMarshalSchema(getDownloadURLSchema),
				},
				Handler: h.getDownloadURLMCP,
			},
		)
		ts.HTTP = append(ts.HTTP,
			HTTPRouteSpec{
				Path:                "/storage/read_file",
				Handler:             h.readFileHTTP,
				RequiresPermission:  true,
				RequiredMethodNames: []string{"GetDownloadURLs"},
			},
			HTTPRouteSpec{
				Path:                "/storage/get_download_url",
				Handler:             h.getDownloadURLHTTP,
				RequiresPermission:  true,
				RequiredMethodNames: []string{"GetDownloadURLs"},
			},
		)
		ts.OpenAPI = append(ts.OpenAPI,
			makeStorageOpenAPISpec(
				"/storage/read_file", "Read file content", "storage_read_file", readFileSchema),
			makeStorageOpenAPISpec(
				"/storage/get_download_url", "Get download URL", "storage_get_download_url", getDownloadURLSchema),
		)
	}

	appendStorageTool(ts, allowedMethods, storageToolRegistration{
		requiredMethods: []string{"ReserveDataset", "CommitDataset", "DeleteDataset"},
		name:            "storage_put_file",
		title:           "Put File",
		description:     "Write content to a file in a new dataset.",
		schema:          putFileSchema,
		mcpHandler:      h.putFileMCP,
		path:            "/storage/put_file",
		httpHandler:     h.putFileHTTP,
		summary:         "Write file content",
	})

	appendStorageTool(ts, allowedMethods, storageToolRegistration{
		requiredMethods: []string{"DeleteDataset"},
		name:            "storage_delete_dataset",
		title:           "Delete Dataset",
		description:     "Delete a dataset by ID.",
		schema:          deleteDatasetSchema,
		mcpHandler:      h.deleteDatasetMCP,
		path:            "/storage/delete_dataset",
		httpHandler:     h.deleteDatasetHTTP,
		summary:         "Delete a dataset",
	})

	return ts
}

type storageToolRegistration struct {
	requiredMethods []string
	name            string
	title           string
	description     string
	schema          map[string]any
	mcpHandler      mcp.ToolHandler
	path            string
	httpHandler     http.HandlerFunc
	summary         string
}

func appendStorageTool(ts *ToolSet, allowed map[string]struct{}, reg storageToolRegistration) {
	if !hasStorageMethods(allowed, reg.requiredMethods...) {
		return
	}

	ts.MCP = append(ts.MCP, MCPToolSpec{
		Tool: &mcp.Tool{
			Name:        reg.name,
			Title:       reg.title,
			Description: reg.description,
			InputSchema: mustMarshalSchema(reg.schema),
		},
		Handler: reg.mcpHandler,
	})
	ts.HTTP = append(ts.HTTP, HTTPRouteSpec{
		Path:                reg.path,
		Handler:             reg.httpHandler,
		RequiresPermission:  true,
		RequiredMethodNames: reg.requiredMethods,
	})
	ts.OpenAPI = append(ts.OpenAPI, makeStorageOpenAPISpec(reg.path, reg.summary, reg.name, reg.schema))
}

func makeStorageOpenAPISpec(path, summary, operationID string, schema map[string]any) OpenAPIPathSpec {
	return OpenAPIPathSpec{
		Path: path,
		PathItem: map[string]any{
			"post": map[string]any{
				"summary":     summary,
				"operationId": operationID,
				"requestBody": map[string]any{
					"required": true,
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": schema,
						},
					},
				},
				"responses": map[string]any{
					"200": map[string]any{
						"description": "Successful response",
					},
				},
			},
		},
	}
}

func storageAllowedMethodNames(methods []proto.MethodInfo) map[string]struct{} {
	allowed := make(map[string]struct{})

	for _, method := range methods {
		if method.Service == nil || method.Method == nil {
			continue
		}

		if string(method.Service.FullName()) != storageBucketGatewayService {
			continue
		}

		allowed[strings.TrimSpace(string(method.Method.Name()))] = struct{}{}
	}

	return allowed
}

func hasStorageMethods(allowed map[string]struct{}, required ...string) bool {
	if len(required) == 0 {
		return true
	}

	for _, name := range required {
		if _, ok := allowed[name]; !ok {
			return false
		}
	}

	return true
}

// StorageDocAliases returns docs aliases for custom storage tools.
func StorageDocAliases(methods []proto.MethodInfo) docs.ToolAliases {
	rawTools := make(map[string]string)

	for _, method := range methods {
		if method.Service == nil || method.Method == nil {
			continue
		}

		if string(method.Service.FullName()) != storageBucketGatewayService {
			continue
		}

		rawTools[string(method.Method.Name())] = method.ToolName
	}

	aliases := docs.ToolAliases{}
	addStorageDocAlias(aliases, "storage_list_datasets", rawTools["ListDatasets"])
	addStorageDocAlias(aliases, "storage_list_files", rawTools["GetDatasetContents"])
	addStorageDocAlias(aliases, "storage_read_file", rawTools["GetDownloadURLs"])
	addStorageDocAlias(aliases, "storage_get_download_url", rawTools["GetDownloadURLs"])
	addStorageDocAlias(aliases, "storage_put_file",
		rawTools["ReserveDataset"],
		rawTools["CommitDataset"],
		rawTools["DeleteDataset"],
	)
	addStorageDocAlias(aliases, "storage_delete_dataset", rawTools["DeleteDataset"])

	return aliases
}

func addStorageDocAlias(aliases docs.ToolAliases, exposed string, rawTools ...string) {
	for _, raw := range rawTools {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		aliases[exposed] = append(aliases[exposed], raw)
	}
}
