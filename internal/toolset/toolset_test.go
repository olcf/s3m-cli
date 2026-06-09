package toolset

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	storagepb "github.com/olcf/s3m-apis/storage/v1alpha"
	"github.com/olcf/s3m-cli/internal/docs"
	grpcclient "github.com/olcf/s3m-cli/internal/grpc"
	"github.com/olcf/s3m-cli/internal/proto"
)

func TestBuildGRPCToolSetPreparesSpecsAndHandlers(t *testing.T) {
	svc, md := buildEchoDescriptor(t)

	methods := []proto.MethodInfo{{
		File:     md.ParentFile(),
		Service:  svc,
		Method:   md,
		Headers:  []proto.HeaderParam{{Header: "x-test", ParamName: "hdr", Required: true}},
		ToolName: "echo_ping",
		Path:     "/echo/ping",
		Desc:     "call echo",
	}}

	ts := BuildGRPCToolSet(methods, nil, time.Second, false)
	if ts == nil {
		t.Fatal("expected toolset")
	}
	if len(ts.MCP) != 1 || len(ts.HTTP) != 1 || len(ts.OpenAPI) != 1 {
		t.Fatalf("unexpected toolset sizes: %+v", ts)
	}
	if !ts.HTTP[0].RequiresPermission {
		t.Fatal("expected HTTP route to require permission checks")
	}
	if ts.HTTP[0].Method.Method == nil || string(ts.HTTP[0].Method.Method.Name()) != "Ping" {
		t.Fatalf("expected HTTP route method metadata, got %+v", ts.HTTP[0].Method)
	}

	spec := ts.MCP[0]
	schema, ok := spec.Tool.InputSchema.(json.RawMessage)
	if spec.Tool.Name != "echo_ping" || spec.Tool.Title == "" || !ok || len(schema) == 0 {
		t.Fatalf("tool spec not populated: %+v", spec.Tool)
	}

	// Missing required header should surface an MCP error result.
	res, err := spec.Handler(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{}`)},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "missing required parameter") {
		t.Fatalf("expected error result, got %+v", res)
	}

	// When a connection is not configured, the handler should return a dry-run message.
	res, err = spec.Handler(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"hdr":"abc","msg":"hello"}`)},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if res.IsError || !strings.Contains(text, "dry-run") || !strings.Contains(text, `"msg":"hello"`) {
		t.Fatalf("unexpected dry-run result: %+v", res)
	}
}

func TestMakeOpenAPIHandlerInvokesGRPCAndHandlesCORS(t *testing.T) {
	conn, md := startEchoServer(t)
	headers := []proto.HeaderParam{{Header: "x-test", ParamName: "hdr", Required: true}}

	handler := makeOpenAPIHandler(
		grpcclient.MethodKey{ServiceFull: "test.v1.Echo", Method: "Ping"},
		md,
		conn,
		headers,
		time.Second,
		false,
	)

	t.Run("success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewBufferString(`{"hdr":"token","msg":"hi"}`))
		req.Header.Set("Origin", "http://example.com")

		rr := httptest.NewRecorder()
		handler(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Fatalf("content type missing: %s", ct)
		}

		if rr.Header().Get("Access-Control-Allow-Origin") != "http://example.com" {
			t.Fatalf("cors header not echoed: %+v", rr.Header())
		}

		var resp map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("parse response: %v", err)
		}
		if resp["reply"] != "ack:hi" {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("missing header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewBufferString(`{"msg":"hi"}`))
		rr := httptest.NewRecorder()

		handler(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("grpc invalid argument", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewBufferString(`{"hdr":"","msg":"hi"}`))
		rr := httptest.NewRecorder()

		handler(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected backend invalid argument to map to 400, got %d: %s", rr.Code, rr.Body.String())
		}

		if !strings.Contains(rr.Body.String(), "missing metadata") {
			t.Fatalf("expected grpc error body, got %q", rr.Body.String())
		}
	})

	t.Run("options", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/echo", nil)
		req.Header.Set("Origin", "http://example.com")
		rr := httptest.NewRecorder()

		handler(rr, req)

		if rr.Code != http.StatusNoContent {
			t.Fatalf("expected 204 for OPTIONS, got %d", rr.Code)
		}
		if rr.Header().Get("Access-Control-Allow-Origin") != "http://example.com" {
			t.Fatalf("cors header missing on OPTIONS: %+v", rr.Header())
		}
	})
}

func TestGenerateOpenAPISpecMergesPaths(t *testing.T) {
	spec := GenerateOpenAPISpec([]OpenAPIPathSpec{
		{Path: "/echo", PathItem: map[string]any{"get": map[string]any{"summary": "a"}}},
		{Path: "/echo", PathItem: map[string]any{"post": map[string]any{"summary": "b"}}},
	})

	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatalf("paths missing from spec: %+v", spec)
	}

	echo, ok := paths["/echo"].(map[string]any)
	if !ok {
		t.Fatalf("echo path missing: %+v", paths)
	}

	if _, ok := echo["get"]; !ok {
		t.Fatal("expected GET operation")
	}
	if _, ok := echo["post"]; !ok {
		t.Fatal("expected POST operation")
	}
}

func TestHTTPStatusForGRPCError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "invalid argument", err: fmt.Errorf("wrap: %w", status.Error(codes.InvalidArgument, "bad")), want: http.StatusBadRequest},
		{name: "unauthenticated", err: fmt.Errorf("wrap: %w", status.Error(codes.Unauthenticated, "bad")), want: http.StatusUnauthorized},
		{name: "permission denied", err: fmt.Errorf("wrap: %w", status.Error(codes.PermissionDenied, "bad")), want: http.StatusForbidden},
		{name: "not found", err: fmt.Errorf("wrap: %w", status.Error(codes.NotFound, "bad")), want: http.StatusNotFound},
		{name: "already exists", err: fmt.Errorf("wrap: %w", status.Error(codes.AlreadyExists, "bad")), want: http.StatusConflict},
		{name: "resource exhausted", err: fmt.Errorf("wrap: %w", status.Error(codes.ResourceExhausted, "bad")), want: http.StatusTooManyRequests},
		{name: "unimplemented", err: fmt.Errorf("wrap: %w", status.Error(codes.Unimplemented, "bad")), want: http.StatusNotImplemented},
		{name: "unavailable", err: fmt.Errorf("wrap: %w", status.Error(codes.Unavailable, "bad")), want: http.StatusServiceUnavailable},
		{name: "deadline exceeded", err: fmt.Errorf("wrap: %w", status.Error(codes.DeadlineExceeded, "bad")), want: http.StatusGatewayTimeout},
		{name: "unknown", err: errors.New("plain error"), want: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := httpStatusForGRPCError(tt.err); got != tt.want {
				t.Fatalf("httpStatusForGRPCError(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

func TestBuildStorageToolSetFiltersByAllowedMethods(t *testing.T) {
	ts := BuildStorageToolSet(new(grpc.ClientConn), time.Second, false, []proto.MethodInfo{
		storageMethodInfo(t, "ListDatasets"),
		storageMethodInfo(t, "GetDownloadURLs"),
	})
	if ts == nil {
		t.Fatal("expected filtered storage toolset")
	}

	mcpTools := make(map[string]struct{}, len(ts.MCP))
	for _, spec := range ts.MCP {
		mcpTools[spec.Tool.Name] = struct{}{}
	}

	for _, name := range []string{"storage_list_datasets", "storage_read_file", "storage_get_download_url"} {
		if _, ok := mcpTools[name]; !ok {
			t.Fatalf("expected MCP tool %q, got %+v", name, mcpTools)
		}
	}

	for _, name := range []string{"storage_list_files", "storage_put_file", "storage_delete_dataset"} {
		if _, ok := mcpTools[name]; ok {
			t.Fatalf("unexpected MCP tool %q in %+v", name, mcpTools)
		}
	}
}

func TestStorageDocAliasesMapCustomToolsToRawMethods(t *testing.T) {
	methods := []proto.MethodInfo{
		storageMethodInfo(t, "ListDatasets"),
		storageMethodInfo(t, "GetDatasetContents"),
		storageMethodInfo(t, "GetDownloadURLs"),
		storageMethodInfo(t, "ReserveDataset"),
		storageMethodInfo(t, "CommitDataset"),
		storageMethodInfo(t, "DeleteDataset"),
	}
	aliases := StorageDocAliases(methods)
	rawByMethod := make(map[string]string, len(methods))

	for _, method := range methods {
		rawByMethod[string(method.Method.Name())] = method.ToolName
	}

	tests := map[string][]string{
		"storage_list_datasets":    {rawByMethod["ListDatasets"]},
		"storage_list_files":       {rawByMethod["GetDatasetContents"]},
		"storage_read_file":        {rawByMethod["GetDownloadURLs"]},
		"storage_get_download_url": {rawByMethod["GetDownloadURLs"]},
		"storage_put_file": {
			rawByMethod["ReserveDataset"],
			rawByMethod["CommitDataset"],
			rawByMethod["DeleteDataset"],
		},
		"storage_delete_dataset": {rawByMethod["DeleteDataset"]},
	}

	for tool, want := range tests {
		got := aliases[tool]
		if !sameStringSet(got, want) {
			t.Fatalf("aliases[%s] = %+v, want %+v", tool, got, want)
		}
	}
}

func TestBuildStorageToolSetRequiresDeletePermissionForPutFile(t *testing.T) {
	ts := BuildStorageToolSet(new(grpc.ClientConn), time.Second, false, []proto.MethodInfo{
		storageMethodInfo(t, "ReserveDataset"),
		storageMethodInfo(t, "CommitDataset"),
	})
	if ts == nil {
		t.Fatal("expected toolset without put_file access")
	}

	for _, spec := range ts.MCP {
		if spec.Tool.Name == "storage_put_file" {
			t.Fatalf("expected put_file to stay hidden without DeleteDataset, got %+v", ts.MCP)
		}
	}

	ts = BuildStorageToolSet(new(grpc.ClientConn), time.Second, false, []proto.MethodInfo{
		storageMethodInfo(t, "ReserveDataset"),
		storageMethodInfo(t, "CommitDataset"),
		storageMethodInfo(t, "DeleteDataset"),
	})
	if ts == nil {
		t.Fatal("expected toolset with put_file access")
	}

	found := false
	for _, spec := range ts.MCP {
		if spec.Tool.Name == "storage_put_file" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected put_file to be exposed, got %+v", ts.MCP)
	}
}

func TestAnnotateToolsWithDocsAppendsNoteOnce(t *testing.T) {
	store := loadDocStore(t, "alpha")

	ts := &ToolSet{
		MCP: []MCPToolSpec{{
			Tool: &mcp.Tool{
				Name:        "alpha",
				Description: "Alpha tool",
			},
		}},
	}

	AnnotateToolsWithDocs(ts, store)
	AnnotateToolsWithDocs(ts, store)

	desc := ts.MCP[0].Tool.Description
	note := `Docs: call doc_lookup with {"tool":"alpha"}.`
	if strings.Count(desc, note) != 1 {
		t.Fatalf("expected doc note once, got %q", desc)
	}
}

func TestWrapStructuredContentHandlesPrimitives(t *testing.T) {
	val, err := wrapStructuredContent([]byte(`"ok"`))
	if err != nil {
		t.Fatalf("wrapStructuredContent error: %v", err)
	}

	m, ok := val.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", val)
	}
	if m["value"] != "ok" {
		t.Fatalf("unexpected value: %+v", m)
	}
}

func TestStorageHandlersUseRawAuthorizationToken(t *testing.T) {
	const token = "plain-token"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != token {
			t.Fatalf("expected raw authorization token, got %q", got)
		}

		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Range", "bytes 0-3/4")
			_, _ = w.Write([]byte("test"))
		case http.MethodPut:
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected method: %s", r.Method)
		}
	}))
	defer srv.Close()

	h := &storageHandlers{timeout: time.Second}
	ctx := grpcclient.ContextWithAuthToken(context.Background(), token)

	content, totalSize, err := h.fetchContentRange(ctx, srv.URL, 0, 4)
	if err != nil {
		t.Fatalf("fetchContentRange: %v", err)
	}

	if string(content) != "test" || totalSize != 4 {
		t.Fatalf("unexpected fetch response: %q %d", content, totalSize)
	}

	if err := h.uploadContent(ctx, srv.URL, []byte("body")); err != nil {
		t.Fatalf("uploadContent: %v", err)
	}
}

func TestHandleReadFileRejectsIgnoredRangeResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Range"); got != "bytes=0-3" {
			t.Fatalf("expected range header %q, got %q", "bytes=0-3", got)
		}

		_, _ = w.Write([]byte("ignored-range-response"))
	}))
	defer srv.Close()

	conn := startStorageGatewayServer(t, &storageGatewayTestServer{
		getDownloadURLsResp: &storagepb.GetDownloadURLsResponse{
			Downloads: []*storagepb.DownloadTarget{{
				Path:        "notes.txt",
				DownloadUrl: srv.URL,
			}},
		},
	})

	h := &storageHandlers{
		conn:    conn,
		timeout: time.Second,
	}

	_, err := h.handleReadFile(context.Background(), readFileRequest{
		DatasetID: "dataset-id",
		Path:      "notes.txt",
		Length:    4,
	})
	if err == nil {
		t.Fatal("expected ignored range response to fail")
	}

	if !strings.Contains(err.Error(), "ignored byte range request") {
		t.Fatalf("expected ignored-range error, got %q", err.Error())
	}
}

func TestHandlePutFileCleansUpReservationAfterUploadFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("expected PUT upload, got %s", r.Method)
		}

		http.Error(w, "upload failed", http.StatusInternalServerError)
	}))
	defer srv.Close()

	gateway := &storageGatewayTestServer{
		reserveResp: &storagepb.ReserveDatasetResponse{
			DatasetId:   "1234567890abcdef1234567890abcdef",
			DatasetName: "dataset-a",
			Uploads: []*storagepb.UploadTarget{{
				Path:      "file.txt",
				UploadUrl: srv.URL,
			}},
		},
	}
	conn := startStorageGatewayServer(t, gateway)

	h := &storageHandlers{
		conn:    conn,
		timeout: time.Second,
	}

	_, err := h.handlePutFile(context.Background(), putFileRequest{
		DatasetName: "dataset-a",
		Path:        "file.txt",
		Content:     "hello",
	})
	if err == nil {
		t.Fatal("expected upload failure")
	}

	if !strings.Contains(err.Error(), "upload content") {
		t.Fatalf("expected wrapped upload error, got %q", err.Error())
	}

	if len(gateway.deleteRequests) != 1 {
		t.Fatalf("expected reserved dataset cleanup, got %d delete calls", len(gateway.deleteRequests))
	}

	if got := gateway.deleteRequests[0].GetDatasetId(); got != "1234567890abcdef1234567890abcdef" {
		t.Fatalf("expected cleanup for reserved dataset, got %q", got)
	}

	if len(gateway.commitRequests) != 0 {
		t.Fatalf("expected commit to be skipped after upload failure, got %d calls", len(gateway.commitRequests))
	}
}

func TestReadFileSchemaDocumentsBytes(t *testing.T) {
	props, ok := readFileSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected read_file schema properties")
	}

	lengthSchema, ok := props["length"].(map[string]any)
	if !ok {
		t.Fatal("expected length schema")
	}

	desc, ok := lengthSchema["description"].(string)
	if !ok {
		t.Fatal("expected length description")
	}

	if !strings.Contains(desc, "bytes") {
		t.Fatalf("expected byte-oriented length description, got %q", desc)
	}

	if strings.Contains(desc, "characters") {
		t.Fatalf("expected character wording to be removed, got %q", desc)
	}
}

// Test to understand current schema marshaling behavior and determine if fallback is needed
func TestBuildGRPCToolSetSchemaBehavior(t *testing.T) {
	svc, md := buildEchoDescriptor(t)

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
	slog.SetDefault(logger)

	methods := []proto.MethodInfo{{
		File:     md.ParentFile(),
		Service:  svc,
		Method:   md,
		Headers:  []proto.HeaderParam{{Header: "x-test", ParamName: "hdr", Required: true}},
		ToolName: "echo_ping",
		Path:     "/echo/ping",
		Desc:     "call echo",
	}}

	ts := BuildGRPCToolSet(methods, nil, time.Second, false)
	if ts == nil {
		t.Fatal("expected toolset")
	}

	// Verify we get a tool
	if len(ts.MCP) != 1 {
		t.Fatalf("expected 1 MCP tool, got %d", len(ts.MCP))
	}

	// Verify the schema is properly generated
	spec := ts.MCP[0]
	schema, ok := spec.Tool.InputSchema.(json.RawMessage)
	if !ok {
		t.Fatal("expected JSON raw message for input schema")
	}

	var schemaMap map[string]any
	if err := json.Unmarshal(schema, &schemaMap); err != nil {
		t.Fatalf("failed to unmarshal schema: %v", err)
	}

	// Should have proper schema structure, not just fallback
	if schemaMap["type"] != "object" {
		t.Fatalf("expected schema type 'object', got %v", schemaMap["type"])
	}

	props, ok := schemaMap["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties in schema")
	}

	if _, hasHeader := props["hdr"]; !hasHeader {
		t.Fatalf("expected hdr property in schema, got %v", props)
	}

	logOutput := logBuf.String()
	if strings.Contains(logOutput, "failed to marshal input schema") {
		t.Fatalf("unexpected schema marshal warning: %s", logOutput)
	}

	t.Run("complex schema", func(t *testing.T) {
		complexFile := &descriptorpb.FileDescriptorProto{
			Name:    gproto.String("complex.proto"),
			Package: gproto.String("test.v1"),
			Syntax:  gproto.String("proto3"),
			MessageType: []*descriptorpb.DescriptorProto{
				{
					Name: gproto.String("ComplexRequest"),
					Field: []*descriptorpb.FieldDescriptorProto{
						{
							Name:     gproto.String("nested"),
							Number:   gproto.Int32(1),
							Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
							Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
							TypeName: gproto.String(".test.v1.ComplexRequest.Nested"),
						},
					},
					NestedType: []*descriptorpb.DescriptorProto{
						{
							Name: gproto.String("Nested"),
							Field: []*descriptorpb.FieldDescriptorProto{
								{
									Name:   gproto.String("value"),
									Number: gproto.Int32(1),
									Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
									Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
								},
							},
						},
					},
				},
			},
			Service: []*descriptorpb.ServiceDescriptorProto{{
				Name: gproto.String("Complex"),
				Method: []*descriptorpb.MethodDescriptorProto{{
					Name:       gproto.String("Process"),
					InputType:  gproto.String(".test.v1.ComplexRequest"),
					OutputType: gproto.String(".test.v1.ComplexRequest"),
				}},
			}},
		}

		files, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{complexFile}})
		if err != nil {
			t.Fatalf("NewFiles: %v", err)
		}

		fd, err := files.FindFileByPath("complex.proto")
		if err != nil {
			t.Fatalf("FindFileByPath: %v", err)
		}

		complexSvc := fd.Services().ByName("Complex")
		complexMd := complexSvc.Methods().ByName("Process")

		complexMethods := []proto.MethodInfo{{
			File:     fd,
			Service:  complexSvc,
			Method:   complexMd,
			Headers:  []proto.HeaderParam{{Header: "x-complex", ParamName: "complex_hdr", Required: true}},
			ToolName: "complex_process",
			Path:     "/complex/process",
			Desc:     "call complex",
		}}

		// Reset log buffer
		logBuf.Reset()

		complexTs := BuildGRPCToolSet(complexMethods, nil, time.Second, false)
		if len(complexTs.MCP) != 1 {
			t.Fatalf("expected 1 MCP tool for complex method, got %d", len(complexTs.MCP))
		}

		complexSchema, ok := complexTs.MCP[0].Tool.InputSchema.(json.RawMessage)
		if !ok {
			t.Fatalf("expected raw schema for complex method, got %T", complexTs.MCP[0].Tool.InputSchema)
		}

		var complexSchemaMap map[string]any
		if err := json.Unmarshal(complexSchema, &complexSchemaMap); err != nil {
			t.Fatalf("failed to unmarshal complex schema: %v", err)
		}

		complexProps, ok := complexSchemaMap["properties"].(map[string]any)
		if !ok {
			t.Fatalf("expected properties in complex schema, got %+v", complexSchemaMap)
		}

		nested, ok := complexProps["nested"].(map[string]any)
		if !ok {
			t.Fatalf("expected nested property schema, got %+v", complexProps["nested"])
		}

		nestedProps, ok := nested["properties"].(map[string]any)
		if !ok || nestedProps["value"] == nil {
			t.Fatalf("expected nested.value property, got %+v", nested)
		}

		complexLogOutput := logBuf.String()
		if strings.Contains(complexLogOutput, "failed to marshal input schema") {
			t.Fatalf("unexpected complex schema marshal warning: %s", complexLogOutput)
		}
	})
}

//
// Test helpers

func buildEchoDescriptor(t *testing.T) (protoreflect.ServiceDescriptor, protoreflect.MethodDescriptor) {
	t.Helper()

	file := &descriptorpb.FileDescriptorProto{
		Name:    gproto.String("echo.proto"),
		Package: gproto.String("test.v1"),
		Syntax:  gproto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: gproto.String("PingRequest"),
				Field: []*descriptorpb.FieldDescriptorProto{{
					Name:   gproto.String("msg"),
					Number: gproto.Int32(1),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				}},
			},
			{
				Name: gproto.String("PingResponse"),
				Field: []*descriptorpb.FieldDescriptorProto{{
					Name:   gproto.String("reply"),
					Number: gproto.Int32(1),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				}},
			},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: gproto.String("Echo"),
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name:       gproto.String("Ping"),
				InputType:  gproto.String(".test.v1.PingRequest"),
				OutputType: gproto.String(".test.v1.PingResponse"),
			}},
		}},
	}

	files, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{file}})
	if err != nil {
		t.Fatalf("NewFiles: %v", err)
	}

	fd, err := files.FindFileByPath("echo.proto")
	if err != nil {
		t.Fatalf("FindFileByPath: %v", err)
	}

	svc := fd.Services().ByName("Echo")
	if svc == nil {
		t.Fatal("service descriptor not found")
	}

	md := svc.Methods().ByName("Ping")
	if md == nil {
		t.Fatal("method descriptor not found")
	}

	return svc, md
}

func startEchoServer(t *testing.T) (*grpc.ClientConn, protoreflect.MethodDescriptor) {
	t.Helper()

	svc, md := buildEchoDescriptor(t)

	lis := bufconn.Listen(1024 * 1024)
	t.Cleanup(func() { _ = lis.Close() })

	srv := grpc.NewServer()
	t.Cleanup(srv.Stop)

	srv.RegisterService(&grpc.ServiceDesc{
		ServiceName: string(svc.FullName()),
		HandlerType: (*struct{})(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: string(md.Name()),
			Handler: func(_ any, ctx context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
				in := dynamicpb.NewMessage(md.Input())
				if err := dec(in); err != nil {
					return nil, err
				}

				mdHeaders, _ := metadata.FromIncomingContext(ctx)
				if got := mdHeaders.Get("x-test"); len(got) != 1 || got[0] == "" {
					return nil, status.Error(codes.InvalidArgument, "missing metadata")
				}

				val := in.Get(md.Input().Fields().ByName("msg")).String()
				out := dynamicpb.NewMessage(md.Output())
				out.Set(md.Output().Fields().ByName("reply"), protoreflect.ValueOfString("ack:"+val))

				return out, nil
			},
		}},
	}, nil)

	go func() {
		_ = srv.Serve(lis)
	}()

	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return conn, md
}

type storageGatewayTestServer struct {
	storagepb.UnimplementedBucketGatewayServer
	reserveResp         *storagepb.ReserveDatasetResponse
	getDownloadURLsResp *storagepb.GetDownloadURLsResponse
	reserveRequests     []*storagepb.ReserveDatasetRequest
	deleteRequests      []*storagepb.DeleteDatasetRequest
	commitRequests      []*storagepb.CommitDatasetRequest
}

func (s *storageGatewayTestServer) ReserveDataset(
	context.Context,
	*storagepb.ReserveDatasetRequest,
) (*storagepb.ReserveDatasetResponse, error) {
	return s.reserveResp, nil
}

func (s *storageGatewayTestServer) CommitDataset(
	ctx context.Context,
	req *storagepb.CommitDatasetRequest,
) (*storagepb.CommitDatasetResponse, error) {
	s.commitRequests = append(s.commitRequests, gproto.Clone(req).(*storagepb.CommitDatasetRequest))

	return &storagepb.CommitDatasetResponse{}, nil
}

func (s *storageGatewayTestServer) DeleteDataset(
	ctx context.Context,
	req *storagepb.DeleteDatasetRequest,
) (*storagepb.DeleteDatasetResponse, error) {
	s.deleteRequests = append(s.deleteRequests, gproto.Clone(req).(*storagepb.DeleteDatasetRequest))

	return &storagepb.DeleteDatasetResponse{}, nil
}

func (s *storageGatewayTestServer) GetDownloadURLs(
	ctx context.Context,
	req *storagepb.GetDownloadURLsRequest,
) (*storagepb.GetDownloadURLsResponse, error) {
	return s.getDownloadURLsResp, nil
}

func startStorageGatewayServer(t *testing.T, srv storagepb.BucketGatewayServer) *grpc.ClientConn {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	t.Cleanup(func() { _ = lis.Close() })

	server := grpc.NewServer()
	t.Cleanup(server.Stop)

	storagepb.RegisterBucketGatewayServer(server, srv)

	go func() {
		_ = server.Serve(lis)
	}()

	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return conn
}

func loadDocStore(t *testing.T, toolName string) *docs.Store {
	t.Helper()

	content := []byte(`---
{"id":"doc1","title":"Doc","tags":["t"],"selectors":[{"tool":"` + toolName + `"}]}
---
Content
`)

	files := fstest.MapFS{
		"docs/doc1.md": &fstest.MapFile{Data: content},
	}

	_, md := buildEchoDescriptor(t)
	methods := []proto.MethodInfo{{
		File:     md.ParentFile(),
		Service:  md.Parent().(protoreflect.ServiceDescriptor),
		Method:   md,
		ToolName: toolName,
		Path:     "/echo/ping",
		Desc:     "call echo",
	}}

	store, err := docs.LoadStore(files, methods)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}

	return store
}

func storageMethodInfo(t *testing.T, methodName string) proto.MethodInfo {
	t.Helper()

	service := storagepb.File_proto_storage_v1alpha_storage_proto.Services().ByName("BucketGateway")
	if service == nil {
		t.Fatal("storage service descriptor not found")
	}

	method := service.Methods().ByName(protoreflect.Name(methodName))
	if method == nil {
		t.Fatalf("storage method %q not found", methodName)
	}

	return proto.MethodInfo{
		File:     storagepb.File_proto_storage_v1alpha_storage_proto,
		Service:  service,
		Method:   method,
		ToolName: proto.ToolNameForMethod(storagepb.File_proto_storage_v1alpha_storage_proto, service, method),
		Path:     proto.BuildRESTPath("storage", "v1alpha", method),
		API:      "storage",
		Version:  "v1alpha",
	}
}

func sameStringSet(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}

	seen := make(map[string]int, len(got))
	for _, value := range got {
		seen[value]++
	}

	for _, value := range want {
		if seen[value] == 0 {
			return false
		}

		seen[value]--
	}

	return true
}
