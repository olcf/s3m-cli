package toolset

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc"

	"github.com/olcf/s3m-cli/internal/docs"
	"github.com/olcf/s3m-cli/internal/proto"
)

func TestExpandDocTextReplacesKnownVariables(t *testing.T) {
	vars := map[string]string{
		"project":      "proj123",
		"auth.enclave": "open",
	}

	got := expandDocText("Account {{project}} on {{auth.enclave}}.", vars)
	want := "Account proj123 on open."

	if got != want {
		t.Fatalf("expandDocText = %q, want %q", got, want)
	}
}

func TestDocSearchToolContract(t *testing.T) {
	store := loadDocStore(t, "alpha")
	ts := BuildDocToolSet(func() *docs.Store { return store }, nil, nil)
	spec := findMCPToolSpec(t, ts, "doc_search")

	result, err := spec.Handler(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{}`)},
	})
	if err != nil {
		t.Fatalf("doc_search handler error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected empty doc_search request to fail, got %+v", result)
	}
	if text := result.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "provide query or tags") {
		t.Fatalf("expected empty doc_search error, got %q", text)
	}
}

func TestDocSearchToolAcceptsQueryOrTags(t *testing.T) {
	store := loadDocStore(t, "alpha")
	ts := BuildDocToolSet(func() *docs.Store { return store }, nil, nil)
	spec := findMCPToolSpec(t, ts, "doc_search")

	tests := []struct {
		name string
		args string
	}{
		{name: "query only", args: `{"query":"content"}`},
		{name: "tags only", args: `{"tags":["t"]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := spec.Handler(context.Background(), &mcp.CallToolRequest{
				Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(tt.args)},
			})
			if err != nil {
				t.Fatalf("doc_search handler error: %v", err)
			}
			if result.IsError {
				t.Fatalf("expected request to succeed, got %+v", result)
			}
		})
	}
}

func TestDocLookupHidesDocsThatOnlyApplyToHiddenTools(t *testing.T) {
	store := loadDocStoreWithDocs(t,
		map[string]docFixture{
			"alpha-only": {Title: "Alpha Doc", Tools: []string{"alpha"}, Body: "alpha content"},
			"beta-only":  {Title: "Beta Doc", Tools: []string{"beta"}, Body: "beta content"},
		},
		"alpha", "beta",
	)

	ts := BuildDocToolSet(func() *docs.Store { return store }, nil, visibleTools("alpha"))
	spec := findMCPToolSpec(t, ts, "doc_lookup")

	result, err := spec.Handler(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"doc_id":"beta-only"}`)},
	})
	if err != nil {
		t.Fatalf("doc_lookup handler error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected hidden doc lookup to fail, got %+v", result)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, `doc_id "beta-only" not found`) {
		t.Fatalf("expected hidden doc lookup to look absent, got %q", text)
	}
}

func TestDocSearchVisibleToolFilteringRespectsPagination(t *testing.T) {
	store := loadDocStoreWithDocs(t,
		map[string]docFixture{
			"alpha-only": {
				Title: "Alpha Doc",
				Tools: []string{"alpha"},
				Body:  "shared content keyword",
			},
			"beta-only": {
				Title: "Beta Doc",
				Tools: []string{"beta"},
				Body:  "shared content keyword",
			},
			"shared": {
				Title: "Shared Doc",
				Tools: []string{"alpha", "beta"},
				Body:  "shared content keyword",
			},
		},
		"alpha", "beta",
	)

	ts := BuildDocToolSet(func() *docs.Store { return store }, nil, visibleTools("alpha"))
	spec := findMCPToolSpec(t, ts, "doc_search")

	result, err := spec.Handler(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Arguments: json.RawMessage(`{"query":"keyword","limit":1,"offset":1}`),
		},
	})
	if err != nil {
		t.Fatalf("doc_search handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected filtered search request to succeed, got %+v", result)
	}

	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}

	var resp docSearchResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(resp.Results) != 1 {
		t.Fatalf("expected one paged visible result, got %+v", resp.Results)
	}
	if resp.Results[0].DocID != "shared" {
		t.Fatalf("expected second visible result to be shared doc, got %+v", resp.Results[0])
	}
	if resp.More {
		t.Fatalf("expected no more visible results, got %+v", resp)
	}
	if resp.NextOffset != 2 {
		t.Fatalf("expected next offset 2, got %d", resp.NextOffset)
	}
}

func TestDocLookupUsesStorageAliases(t *testing.T) {
	getDownloadURLs := storageMethodInfo(t, "GetDownloadURLs")
	methods := []proto.MethodInfo{getDownloadURLs}
	store := loadDocStoreWithFiles(t, methods, StorageDocAliases(methods), map[string][]byte{
		"docs/raw.md": []byte(docFixtureContent("raw", docFixture{
			Title: "Raw Doc",
			Tools: []string{getDownloadURLs.ToolName},
			Body:  "raw storage needle",
		})),
		"docs/alias.md": []byte(docFixtureContent("alias", docFixture{
			Title: "Alias Doc",
			Tools: []string{"storage_read_file"},
			Body:  "alias storage needle",
		})),
	})

	ts := BuildDocToolSet(func() *docs.Store { return store }, nil, visibleTools("storage_read_file"))
	spec := findMCPToolSpec(t, ts, "doc_lookup")

	result, err := spec.Handler(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"tool":"storage_read_file"}`)},
	})
	if err != nil {
		t.Fatalf("doc_lookup handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected alias lookup to succeed, got %+v", result)
	}

	resp := decodeDocLookupResponse(t, result.StructuredContent)
	if len(resp.Docs) != 2 {
		t.Fatalf("expected raw and alias docs, got %+v", resp.Docs)
	}

	gotIDs := map[string]struct{}{}
	for _, doc := range resp.Docs {
		gotIDs[doc.ID] = struct{}{}
		if doc.ID == "raw" && !containsString(doc.AppliesTo, "storage_read_file") {
			t.Fatalf("expected raw doc appliesTo to include storage alias, got %+v", doc.AppliesTo)
		}
	}

	for _, id := range []string{"raw", "alias"} {
		if _, ok := gotIDs[id]; !ok {
			t.Fatalf("expected doc %q in lookup response, got %+v", id, resp.Docs)
		}
	}
}

func TestDocTagsFilterVisibleDocsAndDedupeAliases(t *testing.T) {
	getDownloadURLs := storageMethodInfo(t, "GetDownloadURLs")
	listDatasets := storageMethodInfo(t, "ListDatasets")
	methods := []proto.MethodInfo{getDownloadURLs, listDatasets}
	store := loadDocStoreWithFiles(t, methods, StorageDocAliases(methods), map[string][]byte{
		"docs/download.md": []byte(docFixtureContent("download", docFixture{
			Title: "Download",
			Tools: []string{getDownloadURLs.ToolName},
			Body:  "# Use (download)\nvisible",
		})),
		"docs/list.md": []byte(docFixtureContent("list", docFixture{
			Title: "List",
			Tools: []string{listDatasets.ToolName},
			Body:  "# Use (list)\nhidden",
		})),
	})

	ts := BuildDocToolSet(
		func() *docs.Store { return store },
		nil,
		visibleTools("storage_read_file", "storage_get_download_url"),
	)
	spec := findMCPToolSpec(t, ts, "doc_tags")

	result, err := spec.Handler(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{}`)},
	})
	if err != nil {
		t.Fatalf("doc_tags handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected alias tags to succeed, got %+v", result)
	}

	resp := decodeDocTagsResponse(t, result.StructuredContent)
	counts := map[string]int{}
	for _, tag := range resp.Tags {
		counts[tag.Tag] = tag.Count
	}

	if counts["download"] != 1 {
		t.Fatalf("expected visible alias doc to count once, got %+v", counts)
	}
	if _, ok := counts["list"]; ok {
		t.Fatalf("expected hidden doc tag to be excluded, got %+v", counts)
	}
}

func TestDocSearchHiddenOnlyTagReturnsEmptyResults(t *testing.T) {
	getDownloadURLs := storageMethodInfo(t, "GetDownloadURLs")
	listDatasets := storageMethodInfo(t, "ListDatasets")
	methods := []proto.MethodInfo{getDownloadURLs, listDatasets}
	store := loadDocStoreWithFiles(t, methods, StorageDocAliases(methods), map[string][]byte{
		"docs/download.md": []byte(docFixtureContent("download", docFixture{
			Title: "Download",
			Tools: []string{getDownloadURLs.ToolName},
			Body:  "# Use (download)\nvisible",
		})),
		"docs/list.md": []byte(docFixtureContent("list", docFixture{
			Title: "List",
			Tools: []string{listDatasets.ToolName},
			Body:  "# Use (list)\nhidden",
		})),
	})

	ts := BuildDocToolSet(func() *docs.Store { return store }, nil, visibleTools("storage_read_file"))
	spec := findMCPToolSpec(t, ts, "doc_search")

	result, err := spec.Handler(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"tags":["list"]}`)},
	})
	if err != nil {
		t.Fatalf("doc_search handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected hidden-tag search to return an empty result, got %+v", result)
	}

	resp := decodeDocSearchResponse(t, result.StructuredContent)
	if len(resp.Results) != 0 || resp.More {
		t.Fatalf("expected empty hidden-tag search response, got %+v", resp)
	}
}

func TestDocTagsIncludeUnscopedDocsWhenFiltering(t *testing.T) {
	alpha := storageMethodInfo(t, "GetDownloadURLs")
	methods := []proto.MethodInfo{alpha}
	store := loadDocStoreWithFiles(t, methods, StorageDocAliases(methods), map[string][]byte{
		"docs/general.md": []byte(`---
{"id":"general","title":"General","tags":["general"]}
---
General docs
`),
	})

	ts := BuildDocToolSet(func() *docs.Store { return store }, nil, visibleTools("storage_read_file"))
	spec := findMCPToolSpec(t, ts, "doc_tags")

	result, err := spec.Handler(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{}`)},
	})
	if err != nil {
		t.Fatalf("doc_tags handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected unscoped doc tags to succeed, got %+v", result)
	}

	resp := decodeDocTagsResponse(t, result.StructuredContent)
	if len(resp.Tags) != 1 || resp.Tags[0].Tag != "general" || resp.Tags[0].Count != 1 {
		t.Fatalf("expected unscoped doc tag, got %+v", resp.Tags)
	}
}

func TestStoragePutFileAliasesAllRequiredMethods(t *testing.T) {
	reserve := storageMethodInfo(t, "ReserveDataset")
	commit := storageMethodInfo(t, "CommitDataset")
	deleteDataset := storageMethodInfo(t, "DeleteDataset")
	methods := []proto.MethodInfo{reserve, commit, deleteDataset}
	store := loadDocStoreWithFiles(t, methods, StorageDocAliases(methods), map[string][]byte{
		"docs/put.md": []byte(docFixtureContent("put", docFixture{
			Title: "Put",
			Tools: []string{"storage_put_file"},
			Body:  "put storage doc",
		})),
	})

	doc, ok := store.LookupByID("put")
	if !ok {
		t.Fatal("expected put doc to load")
	}

	for _, name := range []string{
		"storage_put_file",
		reserve.ToolName,
		commit.ToolName,
		deleteDataset.ToolName,
		"storage_delete_dataset",
	} {
		if !containsString(doc.AppliesTo, name) {
			t.Fatalf("expected put doc appliesTo to include %q, got %+v", name, doc.AppliesTo)
		}
	}
}

func TestAnnotateToolsWithDocsUsesStorageAliases(t *testing.T) {
	getDownloadURLs := storageMethodInfo(t, "GetDownloadURLs")
	methods := []proto.MethodInfo{getDownloadURLs}
	store := loadDocStoreWithFiles(t, methods, StorageDocAliases(methods), map[string][]byte{
		"docs/raw.md": []byte(docFixtureContent("raw", docFixture{
			Title: "Raw Doc",
			Tools: []string{getDownloadURLs.ToolName},
			Body:  "raw storage doc",
		})),
	})

	ts := BuildStorageToolSet(new(grpc.ClientConn), 0, false, methods)
	AnnotateToolsWithDocs(ts, store)

	for _, tool := range []string{"storage_read_file", "storage_get_download_url"} {
		spec := findMCPToolSpec(t, ts, tool)
		note := fmt.Sprintf(`Docs: call doc_lookup with {"tool":"%s"}.`, tool)
		if !strings.Contains(spec.Tool.Description, note) {
			t.Fatalf("expected %s to be annotated, got %q", tool, spec.Tool.Description)
		}
	}
}

func findMCPToolSpec(t *testing.T, ts *ToolSet, name string) MCPToolSpec {
	t.Helper()

	for _, spec := range ts.MCP {
		if spec.Tool != nil && spec.Tool.Name == name {
			return spec
		}
	}

	t.Fatalf("tool %q not found", name)
	return MCPToolSpec{}
}

type docFixture struct {
	Title string
	Tools []string
	Body  string
}

func loadDocStoreWithDocs(t *testing.T, fixtures map[string]docFixture, toolNames ...string) *docs.Store {
	t.Helper()

	svc, md := buildEchoDescriptor(t)
	methods := make([]proto.MethodInfo, 0, len(toolNames))

	for _, toolName := range toolNames {
		methods = append(methods, proto.MethodInfo{
			File:     md.ParentFile(),
			Service:  svc,
			Method:   md,
			ToolName: toolName,
			Path:     "/echo/ping",
			Desc:     "call echo",
		})
	}

	files := make(map[string][]byte, len(fixtures))

	for id, fixture := range fixtures {
		files[fmt.Sprintf("docs/%s.md", id)] = []byte(docFixtureContent(id, fixture))
	}

	store, errs := docs.LoadStoreFromFiles(files, methods)
	if len(errs) > 0 {
		t.Fatalf("LoadStoreFromFiles errors: %v", errs)
	}

	return store
}

func docFixtureContent(id string, fixture docFixture) string {
	selectors := make([]string, 0, len(fixture.Tools))

	for _, tool := range fixture.Tools {
		selectors = append(selectors, fmt.Sprintf(`{"tool":"%s"}`, tool))
	}

	return fmt.Sprintf(`---
{"id":"%s","title":"%s","tags":["t"],"selectors":[%s]}
---
%s
`, id, fixture.Title, strings.Join(selectors, ","), fixture.Body)
}

func visibleTools(names ...string) map[string]struct{} {
	visible := make(map[string]struct{}, len(names))

	for _, name := range names {
		visible[strings.ToLower(name)] = struct{}{}
	}

	return visible
}

func loadDocStoreWithFiles(
	t *testing.T,
	methods []proto.MethodInfo,
	aliases docs.ToolAliases,
	files map[string][]byte,
) *docs.Store {
	t.Helper()

	store, errs := docs.LoadStoreFromFiles(files, methods, aliases)
	if len(errs) > 0 {
		t.Fatalf("LoadStoreFromFiles errors: %v", errs)
	}

	return store
}

func decodeDocLookupResponse(t *testing.T, content any) docLookupResponse {
	t.Helper()

	raw, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}

	var resp docLookupResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal lookup response: %v", err)
	}

	return resp
}

func decodeDocSearchResponse(t *testing.T, content any) docSearchResponse {
	t.Helper()

	raw, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}

	var resp docSearchResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal search response: %v", err)
	}

	return resp
}

func decodeDocTagsResponse(t *testing.T, content any) docTagsResponse {
	t.Helper()

	raw, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}

	var resp docTagsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal tags response: %v", err)
	}

	return resp
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}

	return false
}
