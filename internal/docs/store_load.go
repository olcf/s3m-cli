package docs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/olcf/s3m-cli/internal/proto"
)

//
// Store loading

func LoadStore(docsFS fs.FS, methods []proto.MethodInfo, aliases ...ToolAliases) (*Store, error) {
	store := newStore(methods, aliases...)

	err := fs.WalkDir(docsFS, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if d.IsDir() {
			return nil
		}

		if filepath.Ext(d.Name()) != ".md" {
			return nil
		}

		return store.loadDocFileFromFS(docsFS, path)
	})
	if err != nil {
		return nil, fmt.Errorf("load docs: %w", err)
	}

	slog.Info("loaded docs from filesystem", "count", len(store.Docs))

	return store, nil
}

func buildMethodTargets(methods []proto.MethodInfo) []methodTarget {
	out := make([]methodTarget, 0, len(methods))

	for _, m := range methods {
		out = append(out, methodTarget{
			ToolName:    m.ToolName,
			API:         m.API,
			ServiceName: string(m.Service.Name()),
			MethodName:  string(m.Method.Name()),
			Version:     m.Version,
		})
	}

	return out
}

func mergeToolAliases(aliasSets ...ToolAliases) ToolAliases {
	merged := make(ToolAliases)

	for _, aliases := range aliasSets {
		for tool, equivalents := range aliases {
			merged[tool] = append(merged[tool], equivalents...)
		}
	}

	return merged
}

//
// Front matter

func splitFrontMatter(data []byte) ([]byte, []byte, error) {
	const start = "---\n"

	const end = "\n---"

	if !bytes.HasPrefix(data, []byte(start)) {
		return nil, nil, errors.New("missing front matter start '---'")
	}

	rest := data[len(start):]

	meta, body, ok := bytes.Cut(rest, []byte(end))
	if !ok {
		return nil, nil, errors.New("missing front matter terminator '---'")
	}

	if len(body) > 0 && body[0] == '\n' {
		body = body[1:]
	}

	return meta, body, nil
}

//
// Store private methods

func (ds *Store) loadDocFileFromFS(fsys fs.FS, path string) error {
	raw, err := fs.ReadFile(fsys, path)
	if err != nil {
		return fmt.Errorf("read doc %s: %w", path, err)
	}

	return ds.processDocContent(raw, path)
}

func (ds *Store) processDocContent(raw []byte, path string) error {
	fmBytes, bodyBytes, err := splitFrontMatter(raw)
	if err != nil {
		return fmt.Errorf("split doc %s front matter: %w", path, err)
	}

	var fm docFrontMatter
	if err := json.Unmarshal(fmBytes, &fm); err != nil {
		return fmt.Errorf("parse doc %s front matter: %w", path, err)
	}

	if err := ds.validateFrontMatter(&fm); err != nil {
		return fmt.Errorf("validate doc %s: %w", path, err)
	}

	toolNames, err := ds.resolveSelectors(fm.Selectors)
	if err != nil {
		return fmt.Errorf("doc %s selectors: %w", path, err)
	}

	body := strings.TrimSpace(string(bodyBytes))
	doc := &Doc{
		ID:        fm.ID,
		Title:     fm.Title,
		Tags:      append([]string(nil), fm.Tags...),
		Body:      body,
		Path:      path,
		AppliesTo: toolNames,
		Sections:  make([]*DocSection, 0),
		toolLookup: func(names []string) map[string]struct{} {
			m := make(map[string]struct{}, len(names))
			for _, n := range names {
				m[strings.ToLower(n)] = struct{}{}
			}

			return m
		}(toolNames),
	}

	if _, exists := ds.Docs[doc.ID]; exists {
		return fmt.Errorf("duplicate doc id %q", doc.ID)
	}

	doc.Sections = splitDocSections(doc)

	ds.registerDoc(doc, toolNames)

	slog.Info("doc loaded", "id", doc.ID, "path", path, "tools", len(toolNames))

	return nil
}

func (ds *Store) registerDoc(doc *Doc, toolNames []string) {
	ds.Docs[doc.ID] = doc

	for _, tool := range toolNames {
		lower := strings.ToLower(tool)
		ds.toolDocs[lower] = append(ds.toolDocs[lower], doc)
	}

	tagSeen := make(map[string]struct{})
	for _, tag := range doc.Tags {
		tagSeen[tag] = struct{}{}
	}

	for _, section := range doc.Sections {
		for _, tag := range section.Tags {
			tagSeen[tag] = struct{}{}
		}
	}

	for tag := range tagSeen {
		ds.tagCounts[tag]++
	}

	ds.sections = append(ds.sections, doc.Sections...)
}

func (ds *Store) validateFrontMatter(fm *docFrontMatter) error {
	if fm.ID == "" {
		return errors.New("missing id")
	}

	for i, sel := range fm.Selectors {
		hasTool := strings.TrimSpace(sel.Tool) != ""
		hasOtherConstraints := strings.TrimSpace(sel.API) != "" ||
			strings.TrimSpace(sel.Service) != "" ||
			len(sel.Versions) > 0 ||
			len(sel.Methods) > 0

		if !hasTool && !hasOtherConstraints {
			return fmt.Errorf("selector %d is empty", i)
		}

		if hasTool && hasOtherConstraints {
			return fmt.Errorf("selector %d mixes tool with other constraints", i)
		}
	}

	for _, tag := range fm.Tags {
		if !tagPattern.MatchString(tag) {
			return fmt.Errorf("invalid tag %q", tag)
		}
	}

	return nil
}
