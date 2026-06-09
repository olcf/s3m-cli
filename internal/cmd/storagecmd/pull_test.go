package storagecmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/urfave/cli/v3"
	"google.golang.org/grpc"

	storagepb "github.com/olcf/s3m-apis/storage/v1alpha"
)

func TestResolvePathsNoListing(t *testing.T) {
	client := &fakeStorageClient{
		getDatasetContents: func(context.Context, *storagepb.GetDatasetContentsRequest, ...grpc.CallOption) (*storagepb.GetDatasetContentsResponse, error) {
			return nil, errors.New("unexpected call")
		},
	}

	selector, err := buildDatasetSelector("alpha/2025-01-01", false, false)
	if err != nil {
		t.Fatalf("buildDatasetSelector: %v", err)
	}

	paths, err := resolvePaths(context.Background(), client, selector, "file.txt")
	if err != nil {
		t.Fatalf("resolvePaths: %v", err)
	}
	if len(paths) != 1 || paths[0] != "file.txt" {
		t.Fatalf("unexpected paths: %v", paths)
	}
}

func TestResolvePathsListing(t *testing.T) {
	calls := 0
	client := &fakeStorageClient{
		getDatasetContents: func(_ context.Context, in *storagepb.GetDatasetContentsRequest, _ ...grpc.CallOption) (*storagepb.GetDatasetContentsResponse, error) {
			calls++

			if in.GetGlobPattern() != "dir/*.txt" {
				t.Errorf("expected GlobPattern='dir/*.txt', got: %q", in.GetGlobPattern())
			}

			switch in.GetPageToken() {
			case "":
				return &storagepb.GetDatasetContentsResponse{
					Files: []*storagepb.FileEntry{
						{Path: "dir/a.txt"},
					},
					Pagination: &storagepb.Pagination{
						NextPageToken: "next",
					},
				}, nil
			case "next":
				return &storagepb.GetDatasetContentsResponse{
					Files: []*storagepb.FileEntry{
						{Path: "dir/c.txt"},
					},
				}, nil
			default:
				return nil, errors.New("unexpected page token")
			}
		},
	}

	selector, err := buildDatasetSelector("alpha/2025-01-01", false, false)
	if err != nil {
		t.Fatalf("buildDatasetSelector: %v", err)
	}

	paths, err := resolvePaths(context.Background(), client, selector, "dir/*.txt")
	if err != nil {
		t.Fatalf("resolvePaths: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
	if len(paths) != 2 || paths[0] != "dir/a.txt" || paths[1] != "dir/c.txt" {
		t.Fatalf("unexpected paths: %v", paths)
	}
}

func TestDownloadFile(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.txt")
	content := "payload"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "tok" {
			t.Fatalf("expected Authorization header, got %q", got)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	if err := downloadFile(context.Background(), server.Client(), server.URL, outPath, "tok"); err != nil {
		t.Fatalf("downloadFile: %v", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(got) != content {
		t.Fatalf("unexpected output: %q", string(got))
	}
}

func TestDownloadFileStatusError(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.txt")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	err := downloadFile(context.Background(), server.Client(), server.URL, outPath, "tok")
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("expected HTTP 500 error, got %v", err)
	}
}

func TestDownloadFileAllowsSlowBodyAfterHeaders(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.txt")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "tok" {
			t.Fatalf("expected Authorization header, got %q", got)
		}

		w.WriteHeader(http.StatusOK)

		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		time.Sleep(100 * time.Millisecond)

		if _, err := w.Write([]byte("payload")); err != nil {
			t.Fatalf("write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	client := newTransferHTTPClient(20 * time.Millisecond)
	if err := downloadFile(context.Background(), client, server.URL, outPath, "tok"); err != nil {
		t.Fatalf("downloadFile: %v", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(got) != "payload" {
		t.Fatalf("unexpected output: %q", string(got))
	}
}

func TestWriteFilePreservesExistingFileOnCopyError(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.txt")

	if err := os.WriteFile(outPath, []byte("original"), 0o600); err != nil {
		t.Fatalf("seed output: %v", err)
	}

	err := writeFile(outPath, &failingReader{
		chunks: [][]byte{[]byte("partial")},
		err:    errors.New("copy failed"),
	})
	if err == nil || !strings.Contains(err.Error(), "copy failed") {
		t.Fatalf("expected copy error, got %v", err)
	}

	got, readErr := os.ReadFile(outPath)
	if readErr != nil {
		t.Fatalf("read output: %v", readErr)
	}
	if string(got) != "original" {
		t.Fatalf("expected original content to remain, got %q", string(got))
	}

	matches, globErr := filepath.Glob(filepath.Join(dir, ".out.txt.tmp-*"))
	if globErr != nil {
		t.Fatalf("glob temp files: %v", globErr)
	}
	if len(matches) != 0 {
		t.Fatalf("expected temp files to be cleaned up, found %v", matches)
	}
}

func TestResolvePathForCreateAllowsSymlinkedBase(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	base := filepath.Join(root, "base")

	if err := os.MkdirAll(target, 0o750); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}

	if err := os.Symlink(target, base); err != nil {
		t.Fatalf("symlink base: %v", err)
	}

	resolvedBase, err := resolvePathForCreate(base)
	if err != nil {
		t.Fatalf("resolve base: %v", err)
	}

	resolvedOutDir, err := resolvePathForCreate(filepath.Join(base, "nested"))
	if err != nil {
		t.Fatalf("resolve output dir: %v", err)
	}

	if !pathWithinBase(resolvedBase, resolvedOutDir) {
		t.Fatalf("expected %q to be within %q", resolvedOutDir, resolvedBase)
	}
}

func TestPullDatasetRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "base")
	escape := filepath.Join(root, "escape")

	if err := os.MkdirAll(base, 0o750); err != nil {
		t.Fatalf("mkdir base: %v", err)
	}

	if err := os.MkdirAll(escape, 0o750); err != nil {
		t.Fatalf("mkdir escape: %v", err)
	}

	link := filepath.Join(base, "link")
	if err := os.Symlink(escape, link); err != nil {
		t.Fatalf("symlink link: %v", err)
	}

	client := &fakeStorageClient{
		getDatasetContents: func(context.Context, *storagepb.GetDatasetContentsRequest, ...grpc.CallOption) (*storagepb.GetDatasetContentsResponse, error) {
			return &storagepb.GetDatasetContentsResponse{
				Files: []*storagepb.FileEntry{
					{Path: "link/out.txt"},
				},
			}, nil
		},
		getDownloadURLs: func(context.Context, *storagepb.GetDownloadURLsRequest, ...grpc.CallOption) (*storagepb.GetDownloadURLsResponse, error) {
			return &storagepb.GetDownloadURLsResponse{
				Downloads: []*storagepb.DownloadTarget{
					{
						Path:        "link/out.txt",
						DownloadUrl: "http://example.invalid/download",
					},
				},
			}, nil
		},
	}

	err := pullDataset(context.Background(), client, &storagepb.DatasetSelector{}, "**", base, "tok", &cli.Command{})
	if err == nil || !strings.Contains(err.Error(), "refusing to write outside download directory") {
		t.Fatalf("expected symlink escape rejection, got %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(escape, "out.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected no escaped file, stat err: %v", statErr)
	}
}

func TestPullDatasetDownloadsMatchedFiles(t *testing.T) {
	dir := t.TempDir()
	pathsRequested := []string{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "tok" {
			t.Fatalf("expected Authorization header, got %q", got)
		}

		switch r.URL.Path {
		case "/downloads/dir/a.txt":
			_, _ = w.Write([]byte("alpha"))
		case "/downloads/dir/b.txt":
			_, _ = w.Write([]byte("beta"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := &fakeStorageClient{
		getDatasetContents: func(context.Context, *storagepb.GetDatasetContentsRequest, ...grpc.CallOption) (*storagepb.GetDatasetContentsResponse, error) {
			return &storagepb.GetDatasetContentsResponse{
				Files: []*storagepb.FileEntry{
					{Path: "dir/a.txt"},
					{Path: "dir/b.txt"},
				},
			}, nil
		},
		getDownloadURLs: func(_ context.Context, in *storagepb.GetDownloadURLsRequest, _ ...grpc.CallOption) (*storagepb.GetDownloadURLsResponse, error) {
			pathsRequested = append([]string(nil), in.GetPaths().GetPaths()...)

			return &storagepb.GetDownloadURLsResponse{
				Downloads: []*storagepb.DownloadTarget{
					{Path: "dir/a.txt", DownloadUrl: server.URL + "/downloads/dir/a.txt"},
					{Path: "dir/b.txt", DownloadUrl: server.URL + "/downloads/dir/b.txt"},
				},
			}, nil
		},
	}

	err := pullDataset(context.Background(), client, &storagepb.DatasetSelector{}, "dir/*.txt", dir, "tok", newTestOutputCommand(t))
	if err != nil {
		t.Fatalf("pullDataset: %v", err)
	}

	if !reflect.DeepEqual(pathsRequested, []string{"dir/a.txt", "dir/b.txt"}) {
		t.Fatalf("unexpected download paths: %v", pathsRequested)
	}

	alpha, err := os.ReadFile(filepath.Join(dir, "dir", "a.txt"))
	if err != nil {
		t.Fatalf("read a.txt: %v", err)
	}
	if string(alpha) != "alpha" {
		t.Fatalf("unexpected a.txt content: %q", string(alpha))
	}

	beta, err := os.ReadFile(filepath.Join(dir, "dir", "b.txt"))
	if err != nil {
		t.Fatalf("read b.txt: %v", err)
	}
	if string(beta) != "beta" {
		t.Fatalf("unexpected b.txt content: %q", string(beta))
	}
}

type failingReader struct {
	chunks [][]byte
	err    error
	index  int
}

func (r *failingReader) Read(p []byte) (int, error) {
	if r.index >= len(r.chunks) {
		if r.err == nil {
			return 0, io.EOF
		}

		return 0, r.err
	}

	n := copy(p, r.chunks[r.index])
	r.index++
	if r.index >= len(r.chunks) && r.err != nil {
		return n, fmt.Errorf("%w", r.err)
	}

	return n, nil
}

type fakeStorageClient struct {
	reserveDataset     func(context.Context, *storagepb.ReserveDatasetRequest, ...grpc.CallOption) (*storagepb.ReserveDatasetResponse, error)
	commitDataset      func(context.Context, *storagepb.CommitDatasetRequest, ...grpc.CallOption) (*storagepb.CommitDatasetResponse, error)
	deleteDataset      func(context.Context, *storagepb.DeleteDatasetRequest, ...grpc.CallOption) (*storagepb.DeleteDatasetResponse, error)
	getDatasetContents func(context.Context, *storagepb.GetDatasetContentsRequest, ...grpc.CallOption) (*storagepb.GetDatasetContentsResponse, error)
	getDownloadURLs    func(context.Context, *storagepb.GetDownloadURLsRequest, ...grpc.CallOption) (*storagepb.GetDownloadURLsResponse, error)
}

func (f *fakeStorageClient) ReserveDataset(
	ctx context.Context, in *storagepb.ReserveDatasetRequest, opts ...grpc.CallOption,
) (*storagepb.ReserveDatasetResponse, error) {
	if f.reserveDataset != nil {
		return f.reserveDataset(ctx, in, opts...)
	}

	return nil, errors.New("not implemented")
}

func (f *fakeStorageClient) CommitDataset(
	ctx context.Context, in *storagepb.CommitDatasetRequest, opts ...grpc.CallOption,
) (*storagepb.CommitDatasetResponse, error) {
	if f.commitDataset != nil {
		return f.commitDataset(ctx, in, opts...)
	}

	return nil, errors.New("not implemented")
}

func (f *fakeStorageClient) DeleteDataset(
	ctx context.Context, in *storagepb.DeleteDatasetRequest, opts ...grpc.CallOption,
) (*storagepb.DeleteDatasetResponse, error) {
	if f.deleteDataset != nil {
		return f.deleteDataset(ctx, in, opts...)
	}

	return nil, errors.New("not implemented")
}

func (f *fakeStorageClient) ListDatasets(
	context.Context, *storagepb.ListDatasetsRequest, ...grpc.CallOption,
) (*storagepb.ListDatasetsResponse, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeStorageClient) GetDatasetContents(
	ctx context.Context, in *storagepb.GetDatasetContentsRequest, opts ...grpc.CallOption,
) (*storagepb.GetDatasetContentsResponse, error) {
	if f.getDatasetContents == nil {
		return nil, errors.New("not implemented")
	}

	return f.getDatasetContents(ctx, in, opts...)
}

func (f *fakeStorageClient) GetDownloadURLs(
	ctx context.Context, in *storagepb.GetDownloadURLsRequest, opts ...grpc.CallOption,
) (*storagepb.GetDownloadURLsResponse, error) {
	if f.getDownloadURLs == nil {
		return nil, errors.New("not implemented")
	}

	return f.getDownloadURLs(ctx, in, opts...)
}

func newTestOutputCommand(t *testing.T) *cli.Command {
	t.Helper()

	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "output"},
		},
	}

	if err := cmd.Set("output", "text"); err != nil {
		t.Fatalf("set output flag: %v", err)
	}

	return cmd
}
