package storagecmd

import (
	"context"
	"testing"

	"github.com/urfave/cli/v3"
	"google.golang.org/grpc"

	"github.com/olcf/s3m-cli/internal/output"

	storagepb "github.com/olcf/s3m-apis/storage/v1alpha"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		value int64
		want  string
	}{
		{value: 0, want: "0 B"},
		{value: 500, want: "500 B"},
		{value: 1024, want: "1.0 KiB"},
		{value: 1536, want: "1.5 KiB"},
		{value: 1024 * 1024, want: "1.0 MiB"},
		{value: 5 * 1024 * 1024, want: "5.0 MiB"},
	}

	for _, tt := range tests {
		if got := output.FormatBytes(tt.value); got != tt.want {
			t.Fatalf("output.FormatBytes(%d) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

//
// Mock client for pagination tests

type mockListClient struct {
	listDatasetsRequest        *storagepb.ListDatasetsRequest
	listDatasetsResponse       *storagepb.ListDatasetsResponse
	getDatasetContentsRequest  *storagepb.GetDatasetContentsRequest
	getDatasetContentsResponse *storagepb.GetDatasetContentsResponse
}

func (m *mockListClient) ListDatasets(ctx context.Context, req *storagepb.ListDatasetsRequest, opts ...grpc.CallOption) (*storagepb.ListDatasetsResponse, error) {
	m.listDatasetsRequest = req
	return m.listDatasetsResponse, nil
}

func (m *mockListClient) GetDatasetContents(ctx context.Context, req *storagepb.GetDatasetContentsRequest, opts ...grpc.CallOption) (*storagepb.GetDatasetContentsResponse, error) {
	m.getDatasetContentsRequest = req
	return m.getDatasetContentsResponse, nil
}

func (m *mockListClient) ReserveDataset(ctx context.Context, req *storagepb.ReserveDatasetRequest, opts ...grpc.CallOption) (*storagepb.ReserveDatasetResponse, error) {
	return nil, nil
}

func (m *mockListClient) CommitDataset(ctx context.Context, req *storagepb.CommitDatasetRequest, opts ...grpc.CallOption) (*storagepb.CommitDatasetResponse, error) {
	return &storagepb.CommitDatasetResponse{}, nil
}

func (m *mockListClient) DeleteDataset(ctx context.Context, req *storagepb.DeleteDatasetRequest, opts ...grpc.CallOption) (*storagepb.DeleteDatasetResponse, error) {
	return &storagepb.DeleteDatasetResponse{}, nil
}

func (m *mockListClient) GetDownloadURLs(ctx context.Context, req *storagepb.GetDownloadURLsRequest, opts ...grpc.CallOption) (*storagepb.GetDownloadURLsResponse, error) {
	return nil, nil
}

//
// Dataset listing pagination tests

func TestListDatasets_WithLimitAndOffset(t *testing.T) {
	cmd := newListCommand(t)
	if err := cmd.Set("limit", "20"); err != nil {
		t.Fatalf("set limit: %v", err)
	}
	if err := cmd.Set("offset", "5"); err != nil {
		t.Fatalf("set offset: %v", err)
	}

	client := &mockListClient{
		listDatasetsResponse: &storagepb.ListDatasetsResponse{
			Datasets: []*storagepb.DatasetSummary{},
		},
	}

	if err := listDatasets(context.Background(), client, cmd); err != nil {
		t.Fatalf("listDatasets: %v", err)
	}

	if got := client.listDatasetsRequest.GetPageSize(); got != 20 {
		t.Fatalf("expected PageSize=20, got: %d", got)
	}

	if got := client.listDatasetsRequest.GetOffset(); got != 5 {
		t.Fatalf("expected Offset=5, got: %d", got)
	}
}

//
// File listing pagination tests

func TestListFiles_SelectorShaping(t *testing.T) {
	tests := []struct {
		name           string
		limit          string
		offset         string
		arg            string
		wantGlob       string
		wantPathPrefix string
		wantPageSize   uint32
		wantOffset     uint32
	}{
		{
			name:         "glob pattern with pagination",
			limit:        "10",
			offset:       "2",
			arg:          "*.txt",
			wantGlob:     "*.txt",
			wantPageSize: 10,
			wantOffset:   2,
		},
		{
			name:           "path prefix",
			arg:            "data/logs/",
			wantPathPrefix: "data/logs/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newListCommand(t)
			if tt.limit != "" {
				if err := cmd.Set("limit", tt.limit); err != nil {
					t.Fatalf("set limit: %v", err)
				}
			}
			if tt.offset != "" {
				if err := cmd.Set("offset", tt.offset); err != nil {
					t.Fatalf("set offset: %v", err)
				}
			}

			client := &mockListClient{
				getDatasetContentsResponse: &storagepb.GetDatasetContentsResponse{
					Files: []*storagepb.FileEntry{},
				},
			}

			selector := &storagepb.DatasetSelector{
				Selector: &storagepb.DatasetSelector_DatasetId{
					DatasetId: "12345678901234567890123456789012",
				},
			}

			if err := listDatasetContents(context.Background(), client, selector, "dataset", tt.arg, cmd); err != nil {
				t.Fatalf("listDatasetContents: %v", err)
			}

			req := client.getDatasetContentsRequest
			if got := req.GetGlobPattern(); got != tt.wantGlob {
				t.Fatalf("expected GlobPattern=%q, got: %q", tt.wantGlob, got)
			}
			if got := req.GetPathPrefix(); got != tt.wantPathPrefix {
				t.Fatalf("expected PathPrefix=%q, got: %q", tt.wantPathPrefix, got)
			}
			if got := req.GetPageSize(); got != tt.wantPageSize {
				t.Fatalf("expected PageSize=%d, got: %d", tt.wantPageSize, got)
			}
			if got := req.GetOffset(); got != tt.wantOffset {
				t.Fatalf("expected Offset=%d, got: %d", tt.wantOffset, got)
			}
		})
	}
}

func TestRunLs_RejectsExtraArgs(t *testing.T) {
	client := &mockListClient{}
	cmd := buildLsCommand(nil)
	cmd.Action = func(ctx context.Context, c *cli.Command) error {
		return runLsWithClient(ctx, c, client)
	}

	err := cmd.Run(context.Background(), []string{"ls", "--output", "json", "dataset", "pattern", "extra"})
	if err == nil {
		t.Fatal("expected usage error")
	}

	if got := err.Error(); got != "usage: s3m storage ls [<dataset> [<pattern>]]" {
		t.Fatalf("unexpected error: %q", got)
	}

	if client.listDatasetsRequest != nil || client.getDatasetContentsRequest != nil {
		t.Fatal("expected command to fail before any storage RPC")
	}
}

func TestRunLs_ResolvesShortIDAndPassesSelector(t *testing.T) {
	client := &mockListClient{
		listDatasetsResponse: &storagepb.ListDatasetsResponse{
			Datasets: []*storagepb.DatasetSummary{
				{DatasetId: "abcdef1234567890abcdef1234567890", DatasetName: "dataset-a"},
			},
		},
		getDatasetContentsResponse: &storagepb.GetDatasetContentsResponse{
			Files: []*storagepb.FileEntry{},
		},
	}

	cmd := buildLsCommand(nil)
	cmd.Action = func(ctx context.Context, c *cli.Command) error {
		return runLsWithClient(ctx, c, client)
	}

	err := cmd.Run(context.Background(), []string{"ls", "--output", "json", "--id", "abcd", "logs/"})
	if err != nil {
		t.Fatalf("run ls: %v", err)
	}

	if client.listDatasetsRequest == nil {
		t.Fatal("expected short-id resolution request")
	}

	if client.getDatasetContentsRequest == nil {
		t.Fatal("expected dataset contents request")
	}

	if got := client.getDatasetContentsRequest.GetSelector().GetDatasetId(); got != "abcdef1234567890abcdef1234567890" {
		t.Fatalf("expected resolved dataset ID, got %q", got)
	}

	if got := client.getDatasetContentsRequest.GetPathPrefix(); got != "logs/" {
		t.Fatalf("expected path prefix %q, got %q", "logs/", got)
	}
}

func newListCommand(t *testing.T) *cli.Command {
	t.Helper()

	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.IntFlag{Name: "limit"},
			&cli.IntFlag{Name: "offset"},
			&cli.StringFlag{Name: "output"},
			&cli.BoolFlag{Name: "full-id"},
		},
	}

	if err := cmd.Set("output", "json"); err != nil {
		t.Fatalf("set output: %v", err)
	}

	return cmd
}
