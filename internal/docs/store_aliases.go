package docs

import (
	"sort"
	"strings"
)

//
// Aliases

func newToolAliasIndex(methods []methodTarget, aliases ToolAliases) toolAliasIndex {
	idx := toolAliasIndex{
		equivalents: make(map[string]map[string]struct{}),
		display:     make(map[string]string),
	}

	for _, method := range methods {
		idx.addDisplay(method.ToolName)
	}

	for tool, equivalents := range aliases {
		group := make([]string, 0, len(equivalents)+1)
		group = append(group, tool)
		group = append(group, equivalents...)
		idx.addGroup(group)
	}

	return idx
}

func (idx toolAliasIndex) addGroup(names []string) {
	group := make(map[string]struct{}, len(names))
	seen := make(map[string]struct{}, len(names))

	for _, name := range names {
		norm, display, ok := normalizeToolName(name)
		if !ok {
			continue
		}

		idx.display[norm] = display

		if _, exists := seen[norm]; exists {
			continue
		}

		seen[norm] = struct{}{}
		group[norm] = struct{}{}

		for existing := range idx.equivalents[norm] {
			group[existing] = struct{}{}
		}
	}

	for left := range group {
		idx.equivalents[left] = make(map[string]struct{}, len(group))

		for right := range group {
			idx.equivalents[left][right] = struct{}{}
		}
	}
}

func (idx toolAliasIndex) addDisplay(name string) {
	norm, display, ok := normalizeToolName(name)
	if !ok {
		return
	}

	if _, exists := idx.display[norm]; !exists {
		idx.display[norm] = display
	}
}

func (idx toolAliasIndex) known(name string) bool {
	norm, _, ok := normalizeToolName(name)
	if !ok {
		return false
	}

	_, exists := idx.display[norm]

	return exists
}

func (idx toolAliasIndex) expand(name string) []string {
	norm, display, ok := normalizeToolName(name)
	if !ok {
		return nil
	}

	group := idx.equivalents[norm]
	if len(group) == 0 {
		if stored, exists := idx.display[norm]; exists {
			return []string{stored}
		}

		return []string{display}
	}

	out := make([]string, 0, len(group))
	for item := range group {
		if stored, exists := idx.display[item]; exists {
			out = append(out, stored)
		}
	}

	sort.Strings(out)

	return out
}

func normalizeToolName(name string) (string, string, bool) {
	display := strings.TrimSpace(name)
	if display == "" {
		return "", "", false
	}

	return strings.ToLower(display), display, true
}
