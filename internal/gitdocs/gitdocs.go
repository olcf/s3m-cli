package gitdocs

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"
)

type Config struct {
	URL   string        // Archive URL, such as https://github.com/olcf/s3m-mcp-docs/archive/refs/heads/main.zip
	Token string        // Optional bearer token for private archives
	Poll  time.Duration // 0 = disable update checking
	Path  string        // subdir within archive (default: "docs")
}

type Loader struct {
	cfg      Config
	client   *http.Client
	mu       sync.Mutex
	lastETag string
}

func NewLoader(cfg Config) (*Loader, error) {
	if cfg.URL == "" {
		return nil, errors.New("archive URL is required")
	}

	if cfg.Path == "" {
		cfg.Path = "docs"
	}

	return &Loader{
		cfg: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// Open fetches the archive and returns markdown files as a map.
func (l *Loader) Open(ctx context.Context) (map[string][]byte, string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	data, etag, err := l.fetch(ctx, "")
	if err != nil {
		return nil, "", err
	}

	l.lastETag = etag

	files, err := l.extractMarkdown(data)
	if err != nil {
		return nil, "", err
	}

	return files, etag, nil
}

// Pull fetches updates. Returns (nil, "", nil) if no changes.
func (l *Loader) Pull(ctx context.Context) (map[string][]byte, string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	data, etag, err := l.fetch(ctx, l.lastETag)
	if err != nil {
		return nil, "", err
	}

	// No changes (304 Not Modified)
	if data == nil {
		return nil, "", nil
	}

	l.lastETag = etag

	files, err := l.extractMarkdown(data)
	if err != nil {
		return nil, "", err
	}

	return files, etag, nil
}

func (l *Loader) fetch(ctx context.Context, ifNoneMatch string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.cfg.URL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}

	if l.cfg.Token != "" {
		req.Header.Set("PRIVATE-TOKEN", l.cfg.Token)
	}

	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}

	//nolint:bodyclose
	resp, err := l.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetch archive: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode == http.StatusNotModified {
		return nil, l.lastETag, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("unexpected status: %s", resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read response: %w", err)
	}

	etag := resp.Header.Get("ETag")
	if etag == "" {
		// Fall back to content hash if no ETag
		etag = fmt.Sprintf("%x", sha256.Sum256(data))
	}

	return data, etag, nil
}

func (l *Loader) extractMarkdown(data []byte) (map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}

	files := make(map[string][]byte)

	// Repository archives usually include one top-level directory.
	var rootPrefix string

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}

		if rootPrefix == "" {
			parts := strings.SplitN(f.Name, "/", 2)
			if len(parts) > 1 {
				rootPrefix = parts[0] + "/"
			}
		}

		relPath := strings.TrimPrefix(f.Name, rootPrefix)
		if l.cfg.Path != "" && l.cfg.Path != "." {
			if !strings.HasPrefix(relPath, l.cfg.Path+"/") && relPath != l.cfg.Path {
				continue
			}

			relPath = strings.TrimPrefix(relPath, l.cfg.Path+"/")
		}

		// Only include markdown files
		if !strings.HasSuffix(relPath, ".md") {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", f.Name, err)
		}

		content, err := io.ReadAll(rc)
		_ = rc.Close()

		if err != nil {
			return nil, fmt.Errorf("read %s: %w", f.Name, err)
		}

		key := strings.TrimPrefix(path.Clean(relPath), "./")
		if _, exists := files[key]; exists {
			slog.Warn("git docs path collision: multiple files resolve to the same relative path, later entry wins",
				"key", key, "path", f.Name)
		}

		files[key] = content
	}

	return files, nil
}
