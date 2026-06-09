package docs

import (
	"strings"
	"testing"
)

func TestProcessDocContentRegistersDocAndTags(t *testing.T) {
	store := newTestStore([]methodTarget{{
		ToolName:    "alpha",
		API:         "compute",
		ServiceName: "Scheduler",
		MethodName:  "ListJobs",
		Version:     "v1",
	}})

	raw := []byte(`---
{"id":"doc1","title":"Alpha Doc","tags":["general"],"selectors":[{"api":"compute","service":"Scheduler"}]}
---
# Intro (section-tag)
Alpha content

## Details
More info
`)

	if err := store.processDocContent(raw, "docs/doc1.md"); err != nil {
		t.Fatalf("processDocContent: %v", err)
	}

	doc, ok := store.LookupByID("doc1")
	if !ok {
		t.Fatalf("doc not registered")
	}
	if !doc.AppliesToTool("ALPHA") {
		t.Fatalf("expected AppliesToTool to be case-insensitive")
	}

	if len(doc.Sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(doc.Sections))
	}
	if doc.Sections[0].SectionTitle != "Intro" || len(doc.Sections[0].Tags) != 1 || doc.Sections[0].Tags[0] != "section-tag" {
		t.Fatalf("unexpected section parsing: %+v", doc.Sections[0])
	}

	if len(store.toolDocs["alpha"]) != 1 {
		t.Fatalf("toolDocs entry missing")
	}

	if store.tagCounts["general"] != 1 || store.tagCounts["section-tag"] != 1 {
		t.Fatalf("tagCounts not updated: %+v", store.tagCounts)
	}
}

func TestSearchDocsScoresAndFilters(t *testing.T) {
	store := newTestStore(nil)

	doc1 := &Doc{
		ID:        "a",
		Title:     "Alpha",
		Tags:      []string{"common"},
		Body:      "# Usage (howto)\nKittens run quickly and jump over apples.\n\n# Notes\nApple orchard",
		AppliesTo: []string{"tool-a"},
		toolLookup: map[string]struct{}{
			"tool-a": {},
		},
	}
	doc1.Sections = splitDocSections(doc1)

	doc2 := &Doc{
		ID:        "b",
		Title:     "Beta",
		Tags:      []string{"other"},
		Body:      "# Intro\nKittens everywhere",
		AppliesTo: []string{"tool-b"},
		toolLookup: map[string]struct{}{
			"tool-b": {},
		},
	}
	doc2.Sections = splitDocSections(doc2)

	store.registerDoc(doc1, doc1.AppliesTo)
	store.registerDoc(doc2, doc2.AppliesTo)

	results, more := store.SearchDocs("kittens apples", nil, 1, 0)
	if !more {
		t.Fatalf("expected more=true when more matches exist")
	}
	if len(results) != 1 || results[0].Doc.ID != "a" {
		t.Fatalf("expected highest scoring doc first, got %+v", results)
	}
	if results[0].Sections[0].Section.SectionTitle != "Usage" {
		t.Fatalf("expected top section from highest score, got %+v", results[0].Sections[0].Section)
	}

	results, more = store.SearchDocs("", []string{"common"}, 3, 0)
	if more {
		t.Fatalf("expected more=false when all results returned")
	}
	if len(results) != 1 || results[0].Doc.ID != "a" {
		t.Fatalf("tag filter mismatch: %+v", results)
	}
}

func TestProcessDocContentFailsForUnknownTool(t *testing.T) {
	store := newTestStore([]methodTarget{{
		ToolName:    "alpha",
		API:         "compute",
		ServiceName: "Scheduler",
		MethodName:  "ListJobs",
		Version:     "v1",
	}})

	raw := []byte(`---
{"id":"doc2","title":"Invalid Selector","tags":["general"],"selectors":[{"tool":"missing"}]}
---
Content
`)

	err := store.processDocContent(raw, "docs/doc2.md")
	if err == nil {
		t.Fatal("expected error for missing tool")
	}
	if !strings.Contains(err.Error(), `tool "missing" not found`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProcessDocContentRejectsMixedToolSelector(t *testing.T) {
	store := newTestStore([]methodTarget{{
		ToolName:    "alpha",
		API:         "compute",
		ServiceName: "Scheduler",
		MethodName:  "ListJobs",
		Version:     "v1",
	}})

	raw := []byte(`---
{"id":"doc3","title":"Mixed Selector","selectors":[{"tool":"alpha","api":"compute"}]}
---
Content
`)

	err := store.processDocContent(raw, "docs/doc3.md")
	if err == nil {
		t.Fatal("expected error for mixed selector")
	}
	if !strings.Contains(err.Error(), "mixes tool with other constraints") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProcessDocContentExpandsToolAliases(t *testing.T) {
	tests := []struct {
		name      string
		id        string
		raw       string
		lookupKey string
	}{
		{
			name: "alias selector",
			id:   "doc4",
			raw: `---
{"id":"doc4","title":"Alias Selector","tags":["storage"],"selectors":[{"tool":"storage_read_file"}]}
---
Content
`,
			lookupKey: "raw_read",
		},
		{
			name: "method selector",
			id:   "doc7",
			raw: `---
{"id":"doc7","title":"Raw Selector","tags":["storage"],"selectors":[{"api":"storage","methods":["GetDownloadURLs"]}]}
---
Content
`,
			lookupKey: "storage_read_file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestStore([]methodTarget{{
				ToolName:    "raw_read",
				API:         "storage",
				ServiceName: "BucketGateway",
				MethodName:  "GetDownloadURLs",
				Version:     "v1alpha",
			}})
			store.aliases = newToolAliasIndex(store.methodMeta, ToolAliases{
				"storage_read_file": {"raw_read"},
			})

			if err := store.processDocContent([]byte(tt.raw), "docs/"+tt.id+".md"); err != nil {
				t.Fatalf("processDocContent: %v", err)
			}

			doc, ok := store.LookupByID(tt.id)
			if !ok {
				t.Fatal("expected doc to load")
			}

			wantAppliesTo := []string{"raw_read", "storage_read_file"}
			if strings.Join(doc.AppliesTo, ",") != strings.Join(wantAppliesTo, ",") {
				t.Fatalf("expected deterministic expanded appliesTo %+v, got %+v", wantAppliesTo, doc.AppliesTo)
			}

			if docs := store.LookupByTool(tt.lookupKey); len(docs) != 1 || docs[0].ID != tt.id {
				t.Fatalf("expected %q lookup to find doc, got %+v", tt.lookupKey, docs)
			}
		})
	}
}

func TestProcessDocContentRejectsUnknownToolAlias(t *testing.T) {
	store := newTestStore([]methodTarget{{
		ToolName:    "raw_read",
		API:         "storage",
		ServiceName: "BucketGateway",
		MethodName:  "GetDownloadURLs",
		Version:     "v1alpha",
	}})
	store.aliases = newToolAliasIndex(store.methodMeta, ToolAliases{
		"storage_read_file": {"raw_read"},
	})

	raw := []byte(`---
{"id":"doc5","title":"Bad Alias","tags":["storage"],"selectors":[{"tool":"storage_raed_file"}]}
---
Content
`)

	err := store.processDocContent(raw, "docs/doc5.md")
	if err == nil {
		t.Fatal("expected error for unknown alias")
	}
	if !strings.Contains(err.Error(), `tool "storage_raed_file" not found`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProcessDocContentAllowsUnscopedDocs(t *testing.T) {
	store := newTestStore(nil)

	raw := []byte(`---
{"id":"doc6","title":"General Doc","tags":["general"]}
---
General content
`)

	if err := store.processDocContent(raw, "docs/doc6.md"); err != nil {
		t.Fatalf("processDocContent: %v", err)
	}

	doc, ok := store.LookupByID("doc6")
	if !ok {
		t.Fatal("expected unscoped doc to load")
	}
	if len(doc.AppliesTo) != 0 {
		t.Fatalf("expected no applies-to tools, got %+v", doc.AppliesTo)
	}
}

func newTestStore(meta []methodTarget) *Store {
	return &Store{
		Docs:       make(map[string]*Doc),
		toolDocs:   make(map[string][]*Doc),
		tagCounts:  make(map[string]int),
		sections:   make([]*DocSection, 0),
		methodMeta: meta,
		aliases:    newToolAliasIndex(meta, nil),
	}
}
