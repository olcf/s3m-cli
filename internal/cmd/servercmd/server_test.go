package servercmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	slurmv0042pb "github.com/olcf/s3m-apis/slurm/v0042"
	"github.com/urfave/cli/v3"

	"github.com/olcf/s3m-cli/internal/auth"
	"github.com/olcf/s3m-cli/internal/docs"
	"github.com/olcf/s3m-cli/internal/proto"
	"github.com/olcf/s3m-cli/internal/runtime"
)

func TestStatelessMCPHTTPHandlerUsesBearerForVisibleDocs(t *testing.T) {
	file := slurmv0042pb.File_proto_slurm_v0042_slurm_proto
	service := file.Services().ByName("SlurmIndirect")
	method := service.Methods().ByName("GetJobs")
	toolName := proto.ToolNameForMethod(file, service, method)

	methods := []proto.MethodInfo{{
		File:     file,
		Service:  service,
		Method:   method,
		ToolName: toolName,
		Path:     "/jobs",
		Desc:     "Get jobs",
	}}

	store := loadToolDocStore(t, methods, toolName, "Project {{project}}")
	rt := &runtime.Runtime{Methods: methods}
	rt.SetDocs(store)

	cache := newTokenCache(rt)
	cache.storeRecord("unknown-token", auth.TokenRecord{
		Token:                  "unknown-token",
		Project:                "proj-x",
		Enclave:                "enc",
		LastIntrospectionError: "unavailable",
	})

	server := httptest.NewServer(newStatelessMCPHTTPHandler(rt, nil, cache))
	defer server.Close()

	httpClient := server.Client()
	httpClient.Transport = authHeaderRoundTripper{
		base:  httpClient.Transport,
		token: "unknown-token",
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             server.URL,
		HTTPClient:           httpClient,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if !hasTool(tools.Tools, toolName) {
		t.Fatalf("expected fail-open stateless surface to include %q, got %+v", toolName, toolNames(tools.Tools))
	}
	if !hasTool(tools.Tools, "doc_lookup") {
		t.Fatalf("expected doc_lookup to be exposed, got %+v", toolNames(tools.Tools))
	}

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "doc_lookup",
		Arguments: map[string]any{"tool": toolName},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected doc_lookup to succeed, got %+v", result)
	}

	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected text content, got %T", result.Content[0])
	}
	if !strings.Contains(text.Text, "proj-x") {
		t.Fatalf("expected doc content to expand token-scoped vars, got %q", text.Text)
	}
}

func TestMergeGitDocsReplacesRuntimeDocs(t *testing.T) {
	file := slurmv0042pb.File_proto_slurm_v0042_slurm_proto
	service := file.Services().ByName("SlurmIndirect")
	method := service.Methods().ByName("GetJobs")
	toolName := proto.ToolNameForMethod(file, service, method)

	methods := []proto.MethodInfo{{
		File:     file,
		Service:  service,
		Method:   method,
		ToolName: toolName,
		Path:     "/jobs",
		Desc:     "Get jobs",
	}}

	rt := &runtime.Runtime{Methods: methods}
	rt.SetDocs(loadToolDocStoreWithID(t, methods, "doc1", toolName, "Old doc"))

	if err := mergeGitDocs(rt, map[string][]byte{
		"docs/doc2.md": toolDocContent("doc2", toolName, "New doc"),
	}); err != nil {
		t.Fatalf("mergeGitDocs: %v", err)
	}

	store := rt.GetDocs()
	if store == nil {
		t.Fatal("expected git docs store to be set")
	}
	if _, ok := store.LookupByID("doc1"); ok {
		t.Fatal("expected previous runtime doc to be replaced")
	}
	if doc, ok := store.LookupByID("doc2"); !ok || !strings.Contains(doc.Body, "New doc") {
		t.Fatalf("expected replacement git doc to be loaded, got %+v ok=%v", doc, ok)
	}
}

func TestServerCommandsDefaultToPublicGitDocs(t *testing.T) {
	rt := &runtime.Runtime{}

	for _, tt := range []struct {
		name  string
		build func(*runtime.Runtime) *cli.Command
	}{
		{name: "mcp", build: BuildMCPCommand},
		{name: "openapi", build: BuildOpenAPICommand},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.build(rt)

			if got := stringFlagDefault(t, cmd, "docs-url"); got != defaultDocsArchiveURL {
				t.Fatalf("docs-url default = %q, want %q", got, defaultDocsArchiveURL)
			}
			if got := stringFlagDefault(t, cmd, "docs-path"); got != defaultDocsPath {
				t.Fatalf("docs-path default = %q, want %q", got, defaultDocsPath)
			}
			if !hasFlag(cmd, "docs-token") || !hasFlag(cmd, "docs-poll") {
				t.Fatalf("expected docs override flags on %s command", tt.name)
			}
		})
	}
}

type authHeaderRoundTripper struct {
	base  http.RoundTripper
	token string
}

func (rt authHeaderRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.Header = req.Header.Clone()
	cloned.Header.Set("Authorization", "Bearer "+rt.token)

	return rt.base.RoundTrip(cloned)
}

func loadToolDocStore(
	t *testing.T,
	methods []proto.MethodInfo,
	toolName string,
	body string,
) *docs.Store {
	return loadToolDocStoreWithID(t, methods, "doc1", toolName, body)
}

func loadToolDocStoreWithID(
	t *testing.T,
	methods []proto.MethodInfo,
	docID string,
	toolName string,
	body string,
) *docs.Store {
	t.Helper()

	store, errs := docs.LoadStoreFromFiles(map[string][]byte{
		fmt.Sprintf("docs/%s.md", docID): toolDocContent(docID, toolName, body),
	}, methods)
	if len(errs) > 0 {
		t.Fatalf("LoadStoreFromFiles errors: %v", errs)
	}

	return store
}

func toolDocContent(docID string, toolName string, body string) []byte {
	return []byte(fmt.Sprintf(`---
{"id":"%s","title":"Doc","tags":["t"],"selectors":[{"tool":"%s"}]}
---
%s
`, docID, toolName, body))
}

func hasTool(tools []*mcp.Tool, name string) bool {
	for _, tool := range tools {
		if tool != nil && tool.Name == name {
			return true
		}
	}

	return false
}

func toolNames(tools []*mcp.Tool) []string {
	names := make([]string, 0, len(tools))

	for _, tool := range tools {
		if tool == nil {
			continue
		}

		names = append(names, tool.Name)
	}

	return names
}

func stringFlagDefault(t *testing.T, cmd *cli.Command, name string) string {
	t.Helper()

	for _, flag := range cmd.Flags {
		if stringFlag, ok := flag.(*cli.StringFlag); ok && stringFlag.Name == name {
			return stringFlag.Value
		}
	}

	t.Fatalf("string flag %q not found", name)
	return ""
}

func hasFlag(cmd *cli.Command, name string) bool {
	for _, flag := range cmd.Flags {
		switch typed := flag.(type) {
		case *cli.StringFlag:
			if typed.Name == name {
				return true
			}
		case *cli.DurationFlag:
			if typed.Name == name {
				return true
			}
		}
	}

	return false
}
