package toolset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/olcf/s3m-cli/internal/docs"
)

//
// Types

type DocStoreGetter func() *docs.Store

type docHandlers struct {
	getStore     DocStoreGetter
	vars         DocVariableProvider
	visibleTools map[string]struct{}
}

func (h *docHandlers) currentVars(ctx context.Context) map[string]string {
	if h == nil || h.vars == nil {
		return nil
	}

	vars := h.vars(ctx)
	if vars == nil {
		return nil
	}

	// Ensure callers cannot mutate the provider's map.
	out := make(map[string]string, len(vars))
	maps.Copy(out, vars)

	return out
}

func (h *docHandlers) docVisible(doc *docs.Doc) bool {
	if doc == nil {
		return false
	}

	if h == nil || h.visibleTools == nil {
		return true
	}

	if len(doc.AppliesTo) == 0 {
		return true
	}

	for _, tool := range doc.AppliesTo {
		if _, ok := h.visibleTools[strings.ToLower(tool)]; ok {
			return true
		}
	}

	return false
}

func (h *docHandlers) filterDocs(docList []*docs.Doc) []*docs.Doc {
	if h == nil || h.visibleTools == nil {
		return docList
	}

	filtered := make([]*docs.Doc, 0, len(docList))

	for _, doc := range docList {
		if h.docVisible(doc) {
			filtered = append(filtered, doc)
		}
	}

	return filtered
}

func (h *docHandlers) visibleDocs(store *docs.Store) []*docs.Doc {
	if store == nil {
		return nil
	}

	docList := make([]*docs.Doc, 0, len(store.Docs))
	for _, doc := range store.Docs {
		docList = append(docList, doc)
	}

	return h.filterDocs(docList)
}

func serveDocHTTP[T any, R any](
	h *docHandlers, w http.ResponseWriter, r *http.Request, handler func(context.Context, T) (R, error),
) {
	var input T

	h.handleHTTP(w, r, http.MethodPost, &input, func() (any, error) {
		resp, err := handler(r.Context(), input)
		if err != nil {
			return nil, err
		}

		return resp, nil
	})
}

func (h *docHandlers) handleHTTP(
	w http.ResponseWriter,
	r *http.Request,
	method string,
	input any,
	handler func() (any, error),
) {
	if !ensureMethodAndCORS(w, r, method) {
		return
	}

	if err := decodeJSONBody(r.Body, input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp, err := handler()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSONResponse(w, resp)
}

func makeDocOpenAPISpec(
	path, summary, operationID string, schema map[string]any, required bool, responseDesc string,
) OpenAPIPathSpec {
	return OpenAPIPathSpec{
		Path: path,
		PathItem: map[string]any{
			"post": map[string]any{
				"summary":     summary,
				"operationId": operationID,
				"requestBody": map[string]any{
					"required": required,
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": schema,
						},
					},
				},
				"responses": map[string]any{
					"200": map[string]any{
						"description": responseDesc,
					},
				},
			},
		},
	}
}

type docLookupRequest struct {
	Tool  string `json:"tool"`
	DocID string `json:"doc_id"` //nolint:tagliatelle // API schema uses doc_id
}

type docLookupResponse struct {
	Docs []docPayload `json:"docs"`
}

type docPayload struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Tags      []string `json:"tags"`
	AppliesTo []string `json:"appliesTo"`
	Content   string   `json:"content"`
}

type docSearchRequest struct {
	Query  string   `json:"query"`
	Limit  int      `json:"limit"`
	Offset int      `json:"offset"`
	Tags   []string `json:"tags"`
}

type docSearchResult struct {
	DocID     string              `json:"doc_id"` //nolint:tagliatelle // API schema uses doc_id
	DocTitle  string              `json:"docTitle"`
	Tags      []string            `json:"tags"`
	AppliesTo []string            `json:"appliesTo"`
	Sections  []docSectionSnippet `json:"sections"`
}

type docSectionSnippet struct {
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
}

type docSearchResponse struct {
	Results    []docSearchResult `json:"results"`
	More       bool              `json:"more"`
	NextOffset int               `json:"nextOffset"`
}

type docTagsRequest struct {
	Prefix string `json:"prefix"`
}

type docTagsResponse struct {
	Tags []docs.TagSummary `json:"tags"`
}

// DocVariableProvider supplies a snapshot of variables that can be expanded in docs.
// It should return a fresh map on each call; callers must not mutate the returned map.
type DocVariableProvider func(context.Context) map[string]string

//
// Tool building

func BuildDocToolSet(getStore DocStoreGetter, vars DocVariableProvider, visibleTools map[string]struct{}) *ToolSet {
	if getStore == nil || getStore() == nil {
		return nil
	}

	h := &docHandlers{
		getStore:     getStore,
		vars:         vars,
		visibleTools: cloneVisibleTools(visibleTools),
	}
	ts := New()

	ts.MCP = append(ts.MCP,
		MCPToolSpec{
			Tool: &mcp.Tool{
				Name:        "doc_lookup",
				Title:       "Doc Lookup",
				Description: "Retrieve Markdown docs associated with a tool or doc id.",
				InputSchema: mustMarshalSchema(lookupSchemaMap),
			},
			Handler: h.lookupMCP,
		},
		MCPToolSpec{
			Tool: &mcp.Tool{
				Name:        "doc_search",
				Title:       "Doc Search",
				Description: "Search doc sections by keyword.",
				InputSchema: mustMarshalSchema(searchSchemaMap),
			},
			Handler: h.searchMCP,
		},
		MCPToolSpec{
			Tool: &mcp.Tool{
				Name:        "doc_tags",
				Title:       "Doc Tags",
				Description: "List available documentation tags and their usage counts.",
				InputSchema: mustMarshalSchema(tagsSchemaMap),
			},
			Handler: h.tagsMCP,
		},
	)

	ts.HTTP = append(ts.HTTP,
		HTTPRouteSpec{Path: "/docs/lookup", Handler: h.lookupHTTP},
		HTTPRouteSpec{Path: "/docs/search", Handler: h.searchHTTP},
		HTTPRouteSpec{Path: "/docs/tags", Handler: h.tagsHTTP},
	)

	ts.OpenAPI = append(ts.OpenAPI,
		makeDocOpenAPISpec(
			"/docs/lookup", "Fetch docs for a tool or doc id", "doc_lookup", lookupSchemaMap, true, "Docs retrieved",
		),
		makeDocOpenAPISpec(
			"/docs/search", "Search documentation sections", "doc_search", searchSchemaMap, true, "Search results",
		),
		makeDocOpenAPISpec(
			"/docs/tags", "List documentation tags", "doc_tags", tagsSchemaMap, false, "Tags list",
		),
	)

	return ts
}

func AnnotateToolsWithDocs(toolSet *ToolSet, store *docs.Store) {
	if toolSet == nil || store == nil {
		return
	}

	for _, spec := range toolSet.MCP {
		if spec.Tool == nil {
			continue
		}

		if !store.HasDocs(spec.Tool.Name) {
			continue
		}

		note := fmt.Sprintf("Docs: call doc_lookup with {\"tool\":\"%s\"}.", spec.Tool.Name)

		if strings.Contains(spec.Tool.Description, note) {
			continue
		}

		if spec.Tool.Description != "" && !strings.HasSuffix(spec.Tool.Description, "\n") {
			spec.Tool.Description += "\n"
		}

		spec.Tool.Description += note
	}
}

//
// MCP handlers

func (h *docHandlers) handleMCPRequest(
	ctx context.Context, req *mcp.CallToolRequest, input any, handler func(context.Context) (any, error),
) (*mcp.CallToolResult, error) {
	if err := ctx.Err(); err != nil {
		return ErrorResult(err), nil
	}

	if len(req.Params.Arguments) > 0 {
		if err := json.Unmarshal(req.Params.Arguments, input); err != nil {
			return ErrorResult(fmt.Errorf("parse arguments: %w", err)), nil
		}
	}

	resp, err := handler(ctx)
	if err != nil {
		return ErrorResult(err), nil
	}

	result, err := structuredResult(resp)
	if err != nil {
		return ErrorResult(fmt.Errorf("encode response: %w", err)), nil
	}

	return result, nil
}

func serveDocMCP[T any, R any](
	h *docHandlers,
	ctx context.Context,
	req *mcp.CallToolRequest,
	handler func(context.Context, T) (R, error),
) (*mcp.CallToolResult, error) {
	var input T

	return h.handleMCPRequest(ctx, req, &input, func(ctx context.Context) (any, error) {
		resp, err := handler(ctx, input)
		if err != nil {
			return nil, err
		}

		return resp, nil
	})
}

func buildDocMCP[T any, R any](h *docHandlers, fn func(context.Context, T) (R, error)) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return serveDocMCP(h, ctx, req, fn)
	}
}

func (h *docHandlers) lookupMCP(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return buildDocMCP(h, h.handleLookup)(ctx, req)
}

func (h *docHandlers) searchMCP(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return buildDocMCP(h, h.handleSearch)(ctx, req)
}

func (h *docHandlers) tagsMCP(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return buildDocMCP(h, h.handleTags)(ctx, req)
}

//
// HTTP handlers

func (h *docHandlers) lookupHTTP(w http.ResponseWriter, r *http.Request) {
	serveDocHTTP(h, w, r, h.handleLookup)
}

func (h *docHandlers) searchHTTP(w http.ResponseWriter, r *http.Request) {
	serveDocHTTP(h, w, r, h.handleSearch)
}

func (h *docHandlers) tagsHTTP(w http.ResponseWriter, r *http.Request) {
	serveDocHTTP(h, w, r, h.handleTags)
}

//
// Handler logic

func (h *docHandlers) handleLookup(ctx context.Context, input docLookupRequest) (*docLookupResponse, error) {
	if strings.TrimSpace(input.Tool) == "" && strings.TrimSpace(input.DocID) == "" {
		return nil, errors.New("provide tool or doc_id")
	}

	store := h.getStore()
	if store == nil {
		return nil, errors.New("docs not available")
	}

	vars := h.currentVars(ctx)

	var docList []*docs.Doc

	if strings.TrimSpace(input.DocID) != "" {
		doc, ok := store.LookupByID(strings.TrimSpace(input.DocID))
		if !ok {
			return nil, fmt.Errorf("doc_id %q not found", input.DocID)
		}

		if strings.TrimSpace(input.Tool) != "" && !doc.AppliesToTool(input.Tool) {
			return nil, fmt.Errorf("doc_id %q does not apply to tool %q", doc.ID, input.Tool)
		}

		docList = []*docs.Doc{doc}
	} else {
		docList = store.LookupByTool(input.Tool)
	}

	docList = h.filterDocs(docList)
	if len(docList) == 0 {
		if strings.TrimSpace(input.DocID) != "" {
			return nil, fmt.Errorf("doc_id %q not found", input.DocID)
		}

		return nil, fmt.Errorf("no docs for tool %q", input.Tool)
	}

	payloads := make([]docPayload, 0, len(docList))

	for _, d := range docList {
		payloads = append(payloads, docPayload{
			ID:        d.ID,
			Title:     expandDocText(d.Title, vars),
			Tags:      append([]string(nil), d.Tags...),
			AppliesTo: append([]string(nil), d.AppliesTo...),
			Content:   expandDocText(d.Body, vars),
		})
	}

	return &docLookupResponse{Docs: payloads}, nil
}

func (h *docHandlers) handleSearch(ctx context.Context, input docSearchRequest) (*docSearchResponse, error) {
	if strings.TrimSpace(input.Query) == "" && len(input.Tags) == 0 {
		return nil, errors.New("provide query or tags")
	}

	store := h.getStore()
	if store == nil {
		return nil, errors.New("docs not available")
	}

	if input.Limit <= 0 {
		input.Limit = 3
	} else if input.Limit > 10 {
		input.Limit = 10
	}

	if input.Offset < 0 {
		input.Offset = 0
	}

	matches, more := store.SearchDocs(input.Query, input.Tags, input.Limit, input.Offset)
	if h.visibleTools != nil {
		allMatches, _ := store.SearchDocs(input.Query, input.Tags, len(store.Docs), 0)
		matches, more = paginateVisibleDocMatches(h.filterDocMatches(allMatches), input.Limit, input.Offset)
	}

	vars := h.currentVars(ctx)
	results := buildSearchResults(matches, vars)

	return &docSearchResponse{
		Results:    results,
		More:       more,
		NextOffset: input.Offset + len(results),
	}, nil
}

func (h *docHandlers) handleTags(_ context.Context, input docTagsRequest) (*docTagsResponse, error) {
	store := h.getStore()
	if store == nil {
		return nil, errors.New("docs not available")
	}

	summaries := store.TagSummaries(strings.TrimSpace(input.Prefix))
	if h.visibleTools != nil {
		summaries = docs.TagSummariesForDocs(strings.TrimSpace(input.Prefix), h.visibleDocs(store))
	}

	return &docTagsResponse{Tags: summaries}, nil
}

func (h *docHandlers) filterDocMatches(matches []docs.DocMatch) []docs.DocMatch {
	if h == nil || h.visibleTools == nil {
		return matches
	}

	filtered := make([]docs.DocMatch, 0, len(matches))

	for _, match := range matches {
		if h.docVisible(match.Doc) {
			filtered = append(filtered, match)
		}
	}

	return filtered
}

func paginateVisibleDocMatches(matches []docs.DocMatch, limit, offset int) ([]docs.DocMatch, bool) {
	if limit <= 0 {
		limit = 3
	}

	if offset < 0 {
		offset = 0
	}

	if offset >= len(matches) {
		return []docs.DocMatch{}, false
	}

	end := min(offset+limit, len(matches))
	more := end < len(matches)

	return matches[offset:end], more
}

func buildSearchResults(matches []docs.DocMatch, vars map[string]string) []docSearchResult {
	results := make([]docSearchResult, 0, len(matches))

	for _, match := range matches {
		doc := match.Doc
		if doc == nil {
			continue
		}

		sectionHits := match.Sections

		if len(sectionHits) == 0 && len(doc.Sections) > 0 {
			sectionHits = []docs.SectionHit{{Section: doc.Sections[0], Score: match.Score}}
		}

		sections := make([]docSectionSnippet, 0, len(sectionHits))

		for _, hit := range sectionHits {
			if hit.Section == nil {
				continue
			}

			content := expandDocText(hit.Section.Content, vars)

			sections = append(sections, docSectionSnippet{
				Title:   expandDocText(hit.Section.SectionTitle, vars),
				Snippet: truncateSnippet(content),
			})
		}

		results = append(results, docSearchResult{
			DocID:     doc.ID,
			DocTitle:  expandDocText(doc.Title, vars),
			Tags:      append([]string(nil), doc.Tags...),
			AppliesTo: append([]string(nil), doc.AppliesTo...),
			Sections:  sections,
		})
	}

	return results
}

func cloneVisibleTools(visibleTools map[string]struct{}) map[string]struct{} {
	if visibleTools == nil {
		return nil
	}

	out := make(map[string]struct{}, len(visibleTools))

	for name := range visibleTools {
		normalized := strings.ToLower(strings.TrimSpace(name))
		if normalized == "" {
			continue
		}

		out[normalized] = struct{}{}
	}

	return out
}

//
// Utilities

func mustMarshalSchema(schema map[string]any) json.RawMessage {
	raw, err := json.Marshal(schema)
	if err != nil {
		slog.Error("schema marshal failed, using empty object schema", "error", err)
		return json.RawMessage(`{"type":"object"}`)
	}

	return raw
}

func structuredResult(data any) (*mcp.CallToolResult, error) {
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, err
	}

	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(raw)}},
		StructuredContent: data,
	}, nil
}

func ensureMethodAndCORS(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == http.MethodOptions {
		WriteCORSHeaders(w, r)
		w.WriteHeader(http.StatusNoContent)

		return false
	}

	if r.Method != method {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return false
	}

	WriteCORSHeaders(w, r)

	return true
}

func decodeJSONBody(body io.ReadCloser, dst any) error {
	if body == nil {
		return nil
	}

	defer func() {
		if err := body.Close(); err != nil {
			slog.Warn("failed to close body", "error", err)
		}
	}()

	data, err := io.ReadAll(io.LimitReader(body, 1<<20))
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}

	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("parse json: %w", err)
	}

	return nil
}

func writeJSONResponse(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func truncateSnippet(text string) string {
	const limit = 800
	if len(text) <= limit {
		return text
	}

	runes := []rune(text)

	if len(runes) <= limit {
		return text
	}

	return string(runes[:limit]) + "..."
}

var docVarPattern = regexp.MustCompile(`\{\{([a-zA-Z0-9_.-]+)\}\}`)

func expandDocText(text string, vars map[string]string) string {
	if text == "" || len(vars) == 0 {
		return text
	}

	return docVarPattern.ReplaceAllStringFunc(text, func(s string) string {
		matches := docVarPattern.FindStringSubmatch(s)
		if len(matches) != 2 {
			return s
		}

		if val, ok := vars[matches[1]]; ok {
			return val
		}

		return s
	})
}

//
// Schemas

var (
	lookupSchemaMap = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tool": map[string]any{
				"type":        "string",
				"description": "Tool name to retrieve docs for (case-insensitive).",
			},
			"doc_id": map[string]any{
				"type":        "string",
				"description": "Specific doc identifier to fetch.",
			},
		},
		"anyOf": []any{
			map[string]any{"required": []string{"tool"}},
			map[string]any{"required": []string{"doc_id"}},
		},
		"additionalProperties": false,
	}

	searchSchemaMap = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"minLength":   1,
				"pattern":     `.*\S.*`,
				"description": "Keywords to search for.",
			},
			"limit": map[string]any{
				"type":    "integer",
				"minimum": 1,
				"maximum": 10,
				"default": 3,
			},
			"offset": map[string]any{
				"type":    "integer",
				"minimum": 0,
				"default": 0,
			},
			"tags": map[string]any{
				"type":     "array",
				"minItems": 1,
				"items": map[string]any{
					"type":        "string",
					"description": "Filter results to docs containing these tags.",
				},
			},
		},
		"anyOf": []any{
			map[string]any{"required": []string{"query"}},
			map[string]any{"required": []string{"tags"}},
		},
		"additionalProperties": false,
	}

	tagsSchemaMap = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prefix": map[string]any{
				"type":        "string",
				"description": "Optional prefix filter for tags.",
			},
		},
		"additionalProperties": false,
	}
)
