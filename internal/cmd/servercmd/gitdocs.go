package servercmd

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/olcf/s3m-cli/internal/docs"
	"github.com/olcf/s3m-cli/internal/gitdocs"
	"github.com/olcf/s3m-cli/internal/runtime"
	"github.com/olcf/s3m-cli/internal/toolset"
)

const (
	defaultDocsRepoURL    = "https://github.com/olcf/s3m-mcp-docs"
	defaultDocsArchiveURL = defaultDocsRepoURL + "/archive/refs/heads/main.zip"
	defaultDocsPath       = "docs"
	defaultDocsPoll       = 14 * time.Minute
)

// initGitDocs initializes git-based documentation loading.
func initGitDocs(ctx context.Context, rt *runtime.Runtime, cfg *gitdocs.Config) error {
	loader, err := gitdocs.NewLoader(*cfg)
	if err != nil {
		return fmt.Errorf("create loader: %w", err)
	}

	// Initial load
	files, etag, err := loader.Open(ctx)
	if err != nil {
		return fmt.Errorf("open git repo: %w", err)
	}

	if err := mergeGitDocs(rt, files); err != nil {
		return fmt.Errorf("load git docs: %w", err)
	}

	// Start poller if enabled
	if cfg.Poll <= 0 {
		slog.Info("loaded remote docs", "etag", truncateETag(etag), "files", len(files))
		return nil
	}

	slog.Info("loaded remote docs", "etag", truncateETag(etag), "files", len(files), "poll", cfg.Poll)

	go runDocPoller(ctx, rt, loader, cfg.Poll)

	return nil
}

// mergeGitDocs loads docs from git files and replaces the runtime docs store.
func mergeGitDocs(rt *runtime.Runtime, files map[string][]byte) error {
	gitStore, errs := docs.LoadStoreFromFiles(files, rt.Methods, toolset.StorageDocAliases(rt.Methods))
	if len(errs) > 0 {
		for _, e := range errs {
			slog.Error("git doc validation error", "error", e)
		}

		return fmt.Errorf("%d validation errors", len(errs))
	}

	rt.SetDocs(gitStore)

	return nil
}

// runDocPoller periodically pulls updates from the git repository.
func runDocPoller(ctx context.Context, rt *runtime.Runtime, loader *gitdocs.Loader, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	slog.Info("started remote docs poller", "interval", interval)

	for {
		select {
		case <-ctx.Done():
			slog.Info("remote docs poller stopped")
			return
		case <-ticker.C:
			files, etag, err := loader.Pull(ctx)
			if err != nil {
				slog.Warn("remote docs pull failed", "error", err)
				continue // Keep current docs; last good pull
			}

			if files == nil {
				continue // No changes
			}

			if err := mergeGitDocs(rt, files); err != nil {
				slog.Warn("remote docs reload failed", "error", err)
				continue // Keep current docs; last good pull
			}

			slog.Info("refreshed remote docs", "etag", truncateETag(etag), "files", len(files))
		}
	}
}

func truncateETag(etag string) string {
	if len(etag) > 12 {
		return etag[:12]
	}

	return etag
}
