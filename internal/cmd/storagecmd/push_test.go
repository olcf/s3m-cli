package storagecmd

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
	"google.golang.org/grpc"

	"github.com/olcf/s3m-cli/internal/auth"
	"github.com/olcf/s3m-cli/internal/output"
	"github.com/olcf/s3m-cli/internal/runtime"

	storagepb "github.com/olcf/s3m-apis/storage/v1alpha"
)

func TestCollectLocalFilesSingleFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payload.txt")

	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	files, err := collectLocalFiles(path, "")
	if err != nil {
		t.Fatalf("collectLocalFiles: %v", err)
	}

	if got := files["payload.txt"]; got != path {
		t.Fatalf("expected payload.txt=%s, got %q", path, got)
	}
}

func TestCollectLocalFilesSingleFileWithRemotePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payload.txt")

	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	// Test exact remote filename
	files, err := collectLocalFiles(path, "custom/path.txt")
	if err != nil {
		t.Fatalf("collectLocalFiles: %v", err)
	}
	if got := files["custom/path.txt"]; got != path {
		t.Fatalf("expected custom/path.txt=%s, got %q", path, got)
	}

	// Test remote directory (trailing slash)
	files, err = collectLocalFiles(path, "custom/dir/")
	if err != nil {
		t.Fatalf("collectLocalFiles: %v", err)
	}
	if got := files["custom/dir/payload.txt"]; got != path {
		t.Fatalf("expected custom/dir/payload.txt=%s, got %q", path, got)
	}
}

func TestCollectLocalFilesDir(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "nested")
	if err := os.MkdirAll(subdir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	path := filepath.Join(subdir, "file.txt")
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	files, err := collectLocalFiles(dir, "")
	if err != nil {
		t.Fatalf("collectLocalFiles: %v", err)
	}

	if _, ok := files["nested/file.txt"]; !ok {
		t.Fatalf("expected nested file in results: %v", files)
	}
}

func TestCollectLocalFilesDirWithRemotePath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("data"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	files, err := collectLocalFiles(dir, "prefix")
	if err != nil {
		t.Fatalf("collectLocalFiles: %v", err)
	}

	if _, ok := files["prefix/file.txt"]; !ok {
		t.Fatalf("expected prefixed file in results: %v", files)
	}
}

func TestResolveToken(t *testing.T) {
	tests := []struct {
		name       string
		flagToken  string
		stateToken string
		want       string
		wantErr    bool
	}{
		{name: "prefers flag", flagToken: "flag-token", want: "flag-token"},
		{name: "uses state", stateToken: "state-token", want: "state-token"},
		{name: "missing", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := auth.NewState()
			if tt.stateToken != "" {
				if err := state.PutToken(auth.TokenRecord{Token: tt.stateToken, Project: "proj", Enclave: "enc"}); err != nil {
					t.Fatalf("PutToken: %v", err)
				}
			}

			cmd := &cli.Command{
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "token"},
				},
			}
			if tt.flagToken != "" {
				if err := cmd.Set("token", tt.flagToken); err != nil {
					t.Fatalf("set flag: %v", err)
				}
			}

			got, err := resolveToken(cmd, &runtime.Runtime{State: state}, runtime.StorageOpWrite)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error for missing token")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveToken: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestUploadFileSendsAuthHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payload.txt")
	content := "hello world"

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("expected PUT, got %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "tok" {
			t.Fatalf("expected Authorization header, got %q", got)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if string(body) != content {
			t.Fatalf("unexpected body: %q", string(body))
		}

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	if err := uploadFile(context.Background(), server.Client(), path, server.URL, "tok"); err != nil {
		t.Fatalf("uploadFile: %v", err)
	}
}

func TestUploadFileStatusError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payload.txt")

	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	err := uploadFile(context.Background(), server.Client(), path, server.URL, "tok")
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("expected HTTP 500 error, got %v", err)
	}
}

func TestReserveAndUploadCommitsDataset(t *testing.T) {
	dir := t.TempDir()
	localPath := filepath.Join(dir, "payload.txt")

	if err := os.WriteFile(localPath, []byte("hello world"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	reserveCalls := 0
	commitCalls := 0
	deleteCalls := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "tok" {
			t.Fatalf("expected Authorization header, got %q", got)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if string(body) != "hello world" {
			t.Fatalf("unexpected upload body: %q", string(body))
		}

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := &fakeStorageClient{
		reserveDataset: func(_ context.Context, in *storagepb.ReserveDatasetRequest, _ ...grpc.CallOption) (*storagepb.ReserveDatasetResponse, error) {
			reserveCalls++
			if in.GetDatasetName() != "dataset-a" {
				t.Fatalf("unexpected dataset name: %q", in.GetDatasetName())
			}
			if got := in.GetPaths(); len(got) != 1 || got[0] != "remote/payload.txt" {
				t.Fatalf("unexpected reserve paths: %v", got)
			}

			return &storagepb.ReserveDatasetResponse{
				DatasetId:   "dataset-id",
				DatasetName: "dataset-a",
				Uploads: []*storagepb.UploadTarget{{
					Path:      "remote/payload.txt",
					UploadUrl: server.URL,
				}},
			}, nil
		},
		commitDataset: func(_ context.Context, in *storagepb.CommitDatasetRequest, _ ...grpc.CallOption) (*storagepb.CommitDatasetResponse, error) {
			commitCalls++
			if in.GetDatasetId() != "dataset-id" {
				t.Fatalf("unexpected commit dataset id: %q", in.GetDatasetId())
			}

			return &storagepb.CommitDatasetResponse{}, nil
		},
		deleteDataset: func(context.Context, *storagepb.DeleteDatasetRequest, ...grpc.CallOption) (*storagepb.DeleteDatasetResponse, error) {
			deleteCalls++
			return &storagepb.DeleteDatasetResponse{}, nil
		},
	}

	err := reserveAndUpload(
		context.Background(),
		client,
		"dataset-a",
		map[string]string{"remote/payload.txt": localPath},
		"tok",
		newTestOutputCommand(t),
	)
	if err != nil {
		t.Fatalf("reserveAndUpload: %v", err)
	}

	if reserveCalls != 1 {
		t.Fatalf("expected 1 reserve call, got %d", reserveCalls)
	}
	if commitCalls != 1 {
		t.Fatalf("expected 1 commit call, got %d", commitCalls)
	}
	if deleteCalls != 0 {
		t.Fatalf("expected no cleanup delete calls, got %d", deleteCalls)
	}
}

func TestUploadAllCleansUpReservationAfterCanceledUpload(t *testing.T) {
	dir := t.TempDir()
	localPath := filepath.Join(dir, "payload.txt")

	if err := os.WriteFile(localPath, []byte("hello world"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("unexpected upload request")
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	deleteCalls := 0
	cleanupCanceled := false

	client := &fakeStorageClient{
		deleteDataset: func(ctx context.Context, in *storagepb.DeleteDatasetRequest, _ ...grpc.CallOption) (*storagepb.DeleteDatasetResponse, error) {
			deleteCalls++
			if in.GetDatasetId() != "dataset-id" {
				t.Fatalf("unexpected cleanup dataset id: %q", in.GetDatasetId())
			}

			cleanupCanceled = ctx.Err() != nil

			return &storagepb.DeleteDatasetResponse{}, nil
		},
	}

	totalBytes, err := uploadAll(
		ctx,
		client,
		&storagepb.ReserveDatasetResponse{
			DatasetId: "dataset-id",
			Uploads: []*storagepb.UploadTarget{{
				Path:      "payload.txt",
				UploadUrl: server.URL,
			}},
		},
		map[string]string{"payload.txt": localPath},
		"tok",
		output.NewOutput(output.FormatText, io.Discard),
	)
	if err == nil {
		t.Fatal("expected upload failure")
	}
	if !strings.Contains(err.Error(), "upload payload.txt") {
		t.Fatalf("expected wrapped upload error, got %v", err)
	}
	if totalBytes != 0 {
		t.Fatalf("expected zero uploaded bytes on failure, got %d", totalBytes)
	}
	if deleteCalls != 1 {
		t.Fatalf("expected one cleanup delete call, got %d", deleteCalls)
	}
	if cleanupCanceled {
		t.Fatal("expected cleanup to use a detached context")
	}
}
