package docs

import (
	"fmt"
	"log/slog"

	"github.com/olcf/s3m-cli/internal/proto"
)

// LoadStoreFromFiles loads docs from a map of filename to content.
// Returns all validation errors instead of stopping on the first.
func LoadStoreFromFiles(files map[string][]byte, methods []proto.MethodInfo, aliases ...ToolAliases) (*Store, []error) {
	store := newStore(methods, aliases...)

	var errs []error

	for path, content := range files {
		if err := store.processDocContent(content, path); err != nil {
			errs = append(errs, fmt.Errorf("load %s: %w", path, err))
		}
	}

	if len(errs) > 0 {
		return nil, errs
	}

	slog.Info("loaded docs from files", "count", len(store.Docs))

	return store, nil
}
