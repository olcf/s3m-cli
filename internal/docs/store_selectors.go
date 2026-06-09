package docs

import (
	"fmt"
	"sort"
	"strings"
)

//
// Store private methods

func (ds *Store) resolveSelectors(selectors []docSelector) ([]string, error) {
	matches := make(map[string]struct{})

	for _, sel := range selectors {
		names, err := ds.expandSelector(sel)
		if err != nil {
			return nil, err
		}

		for _, name := range names {
			matches[name] = struct{}{}
		}
	}

	if len(matches) == 0 {
		return nil, nil
	}

	final := make([]string, 0, len(matches))
	for name := range matches {
		final = append(final, name)
	}

	sort.Strings(final)

	return final, nil
}

func (ds *Store) expandSelector(sel docSelector) ([]string, error) {
	if sel.Tool != "" {
		return ds.matchTool(sel.Tool)
	}

	names := make([]string, 0)

	for _, target := range ds.methodMeta {
		if sel.API != "" && !strings.EqualFold(sel.API, target.API) {
			continue
		}

		if sel.Service != "" && !strings.EqualFold(sel.Service, target.ServiceName) {
			continue
		}

		if len(sel.Versions) > 0 && !containsFold(sel.Versions, target.Version) {
			continue
		}

		if len(sel.Methods) > 0 && !containsFold(sel.Methods, target.MethodName) {
			continue
		}

		names = append(names, ds.aliases.expand(target.ToolName)...)
	}

	if len(names) == 0 {
		return nil, fmt.Errorf("selector %+v matched no tools", sel)
	}

	return names, nil
}

func (ds *Store) matchTool(name string) ([]string, error) {
	if ds.aliases.known(name) {
		return ds.aliases.expand(name), nil
	}

	return nil, fmt.Errorf("tool %q not found", name)
}

//
// Utilities

func containsFold(list []string, target string) bool {
	for _, item := range list {
		if strings.EqualFold(item, target) {
			return true
		}
	}

	return false
}
