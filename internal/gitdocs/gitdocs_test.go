package gitdocs

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewLoader_MissingURL(t *testing.T) {
	_, err := NewLoader(Config{})
	if err == nil {
		t.Fatal("expected error for missing URL")
	}

	if !strings.Contains(err.Error(), "URL is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewLoader_DefaultPath(t *testing.T) {
	loader, err := NewLoader(Config{URL: "https://example.com/archive.zip"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if loader.cfg.Path != "docs" {
		t.Errorf("expected default path 'docs', got %q", loader.cfg.Path)
	}
}

func createTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}

		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	return buf.Bytes()
}

func TestLoader_Open(t *testing.T) {
	zipData := createTestZip(t, map[string]string{
		"repo-main/docs/test.md":    "# Test\nContent here",
		"repo-main/docs/other.md":   "# Other\nMore content",
		"repo-main/docs/readme.txt": "Not markdown",
		"repo-main/src/code.go":     "package main",
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", "test-etag-123")
		w.Write(zipData)
	}))
	defer server.Close()

	loader, err := NewLoader(Config{
		URL:  server.URL,
		Path: "docs",
	})
	if err != nil {
		t.Fatalf("create loader: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	files, etag, err := loader.Open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if etag != "test-etag-123" {
		t.Errorf("expected etag 'test-etag-123', got %q", etag)
	}

	if len(files) != 2 {
		t.Errorf("expected 2 markdown files, got %d", len(files))
	}

	if _, ok := files["test.md"]; !ok {
		t.Error("expected test.md in files")
	}

	if _, ok := files["other.md"]; !ok {
		t.Error("expected other.md in files")
	}
}

func TestLoader_Pull_NoChanges(t *testing.T) {
	zipData := createTestZip(t, map[string]string{
		"repo-main/docs/test.md": "# Test",
	})

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		if r.Header.Get("If-None-Match") == "test-etag" {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		w.Header().Set("ETag", "test-etag")
		w.Write(zipData)
	}))
	defer server.Close()

	loader, err := NewLoader(Config{
		URL:  server.URL,
		Path: "docs",
	})
	if err != nil {
		t.Fatalf("create loader: %v", err)
	}

	ctx := context.Background()

	// Initial open
	_, _, err = loader.Open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	// Pull - should return nil (no changes)
	files, etag, err := loader.Pull(ctx)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}

	if files != nil {
		t.Error("expected nil files for no-change pull")
	}

	if etag != "" {
		t.Errorf("expected empty etag for no-change pull, got %q", etag)
	}
}

func TestLoader_Pull_WithChanges(t *testing.T) {
	zipData1 := createTestZip(t, map[string]string{
		"repo-main/docs/test.md": "# Test v1",
	})
	zipData2 := createTestZip(t, map[string]string{
		"repo-main/docs/test.md":  "# Test v2",
		"repo-main/docs/test2.md": "# Test 2",
	})

	version := 1
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		etag := "etag-v" + string(rune('0'+version))
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		w.Header().Set("ETag", etag)
		if version == 1 {
			w.Write(zipData1)
		} else {
			w.Write(zipData2)
		}
	}))
	defer server.Close()

	loader, err := NewLoader(Config{
		URL:  server.URL,
		Path: "docs",
	})
	if err != nil {
		t.Fatalf("create loader: %v", err)
	}

	ctx := context.Background()

	// Initial open
	files1, _, err := loader.Open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if len(files1) != 1 {
		t.Errorf("expected 1 file, got %d", len(files1))
	}

	// Change version
	version = 2

	// Pull with changes
	files2, etag, err := loader.Pull(ctx)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}

	if files2 == nil {
		t.Fatal("expected non-nil files for pull with changes")
	}

	if len(files2) != 2 {
		t.Errorf("expected 2 files, got %d", len(files2))
	}

	if etag != "etag-v2" {
		t.Errorf("expected etag 'etag-v2', got %q", etag)
	}
}

func TestLoader_PrivateToken(t *testing.T) {
	zipData := createTestZip(t, map[string]string{
		"repo-main/docs/test.md": "# Test",
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "secret123" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("ETag", "authed")
		w.Write(zipData)
	}))
	defer server.Close()

	// Without auth - should fail
	loaderNoAuth, _ := NewLoader(Config{
		URL:  server.URL,
		Path: "docs",
	})

	ctx := context.Background()
	_, _, err := loaderNoAuth.Open(ctx)
	if err == nil {
		t.Error("expected error without auth")
	}

	// With auth - should succeed
	loaderAuth, _ := NewLoader(Config{
		URL:   server.URL,
		Token: "secret123",
		Path:  "docs",
	})

	files, _, err := loaderAuth.Open(ctx)
	if err != nil {
		t.Fatalf("open with auth: %v", err)
	}

	if len(files) != 1 {
		t.Errorf("expected 1 file, got %d", len(files))
	}
}

func TestLoader_RootPath(t *testing.T) {
	zipData := createTestZip(t, map[string]string{
		"repo-main/readme.md":   "# Readme",
		"repo-main/guide.md":    "# Guide",
		"repo-main/src/code.go": "package main",
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipData)
	}))
	defer server.Close()

	loader, err := NewLoader(Config{
		URL:  server.URL,
		Path: ".", // root path
	})
	if err != nil {
		t.Fatalf("create loader: %v", err)
	}

	ctx := context.Background()
	files, _, err := loader.Open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if len(files) != 2 {
		t.Errorf("expected 2 markdown files at root, got %d", len(files))
	}
}

func TestLoader_OpenPreservesRelativePathsForDuplicateBasenames(t *testing.T) {
	zipData := createTestZip(t, map[string]string{
		"repo-main/docs/guides/readme.md":    "# Guides",
		"repo-main/docs/reference/readme.md": "# Reference",
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipData)
	}))
	defer server.Close()

	loader, err := NewLoader(Config{
		URL:  server.URL,
		Path: "docs",
	})
	if err != nil {
		t.Fatalf("create loader: %v", err)
	}

	files, _, err := loader.Open(context.Background())
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 markdown files, got %d", len(files))
	}

	if _, ok := files["guides/readme.md"]; !ok {
		t.Fatalf("expected guides/readme.md key, got %+v", files)
	}

	if _, ok := files["reference/readme.md"]; !ok {
		t.Fatalf("expected reference/readme.md key, got %+v", files)
	}
}
