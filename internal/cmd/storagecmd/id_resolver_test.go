package storagecmd

import (
	"context"
	"strconv"
	"strings"
	"testing"

	storagepb "github.com/olcf/s3m-apis/storage/v1alpha"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

type mockIDResolverClient struct {
	datasets []*storagepb.DatasetSummary
	pageSize int
	requests []*storagepb.ListDatasetsRequest
}

func (m *mockIDResolverClient) ListDatasets(ctx context.Context, req *storagepb.ListDatasetsRequest, opts ...grpc.CallOption) (*storagepb.ListDatasetsResponse, error) {
	m.requests = append(m.requests, proto.Clone(req).(*storagepb.ListDatasetsRequest))

	datasets := m.datasets
	if prefix := req.GetPrefix(); prefix != "" {
		filtered := make([]*storagepb.DatasetSummary, 0, len(datasets))
		for _, ds := range datasets {
			if strings.HasPrefix(ds.GetDatasetName(), prefix) {
				filtered = append(filtered, ds)
			}
		}

		datasets = filtered
	}

	start := 0
	if req.GetPageToken() != "" {
		n, err := strconv.Atoi(req.GetPageToken())
		if err != nil {
			return nil, err
		}

		start = n
	}

	end := len(datasets)
	if m.pageSize > 0 && start+m.pageSize < end {
		end = start + m.pageSize
	}

	resp := &storagepb.ListDatasetsResponse{
		Datasets: datasets[start:end],
	}
	if end < len(datasets) {
		resp.Pagination = &storagepb.Pagination{
			HasMore:       true,
			NextPageToken: strconv.Itoa(end),
		}
	}

	return resp, nil
}

func (m *mockIDResolverClient) ReserveDataset(ctx context.Context, req *storagepb.ReserveDatasetRequest, opts ...grpc.CallOption) (*storagepb.ReserveDatasetResponse, error) {
	return nil, nil
}

func (m *mockIDResolverClient) CommitDataset(ctx context.Context, req *storagepb.CommitDatasetRequest, opts ...grpc.CallOption) (*storagepb.CommitDatasetResponse, error) {
	return &storagepb.CommitDatasetResponse{}, nil
}

func (m *mockIDResolverClient) DeleteDataset(ctx context.Context, req *storagepb.DeleteDatasetRequest, opts ...grpc.CallOption) (*storagepb.DeleteDatasetResponse, error) {
	return &storagepb.DeleteDatasetResponse{}, nil
}

func (m *mockIDResolverClient) GetDatasetContents(ctx context.Context, req *storagepb.GetDatasetContentsRequest, opts ...grpc.CallOption) (*storagepb.GetDatasetContentsResponse, error) {
	return nil, nil
}

func (m *mockIDResolverClient) GetDownloadURLs(ctx context.Context, req *storagepb.GetDownloadURLsRequest, opts ...grpc.CallOption) (*storagepb.GetDownloadURLsResponse, error) {
	return nil, nil
}

func TestResolveDatasetID(t *testing.T) {
	tests := []struct {
		name            string
		datasets        []*storagepb.DatasetSummary
		input           string
		want            string
		wantErrContains []string
	}{
		{
			name: "full id returned as-is",
			datasets: []*storagepb.DatasetSummary{
				{DatasetId: "abcdef1234567890abcdef1234567890"},
			},
			input: "abcdef1234567890abcdef1234567890",
			want:  "abcdef1234567890abcdef1234567890",
		},
		{
			name: "short id no match",
			datasets: []*storagepb.DatasetSummary{
				{DatasetId: "abcdef1234567890abcdef1234567890"},
			},
			input:           "zzzz",
			wantErrContains: []string{"no dataset found with ID prefix: zzzz"},
		},
		{
			name: "short id ambiguous",
			datasets: []*storagepb.DatasetSummary{
				{DatasetId: "abcd1111567890abcdef1234567890"},
				{DatasetId: "abcd2222567890abcdef1234567890"},
			},
			input:           "abcd",
			wantErrContains: []string{"ambiguous", "multiple datasets"},
		},
		{
			name: "short prefix unique",
			datasets: []*storagepb.DatasetSummary{
				{DatasetId: "abcdef1234567890abcdef1234567890"},
				{DatasetId: "b6cdef1234567890abcdef1234567890"},
			},
			input: "b6",
			want:  "b6cdef1234567890abcdef1234567890",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &mockIDResolverClient{datasets: tt.datasets}

			result, err := resolveDatasetID(context.Background(), client, tt.input)

			if len(tt.wantErrContains) > 0 {
				if err == nil {
					t.Fatalf("Expected error for input %q", tt.input)
				}
				for _, want := range tt.wantErrContains {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("Expected error to contain %q, got: %q", want, err.Error())
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("Expected no error, got: %v", err)
			}
			if result != tt.want {
				t.Errorf("Expected %s, got: %s", tt.want, result)
			}
		})
	}
}

func TestResolveDatasetID_ShortID_SingleMatch(t *testing.T) {
	client := &mockIDResolverClient{
		pageSize: 1,
		datasets: []*storagepb.DatasetSummary{
			{DatasetId: "1234567890abcdef1234567890abcdef", DatasetName: "beta"},
			{DatasetId: "abcdef1234567890abcdef1234567890", DatasetName: "alpha"},
		},
	}

	shortID := "abcd"
	result, err := resolveDatasetID(context.Background(), client, shortID)

	if err != nil {
		t.Fatalf("Expected no error for unique short ID, got: %v", err)
	}

	expected := "abcdef1234567890abcdef1234567890"
	if result != expected {
		t.Errorf("Expected %s, got: %s", expected, result)
	}

	if len(client.requests) != 2 {
		t.Fatalf("expected 2 ListDatasets requests, got %d", len(client.requests))
	}

	if got := client.requests[0].GetPrefix(); got != "" {
		t.Fatalf("expected resolver to avoid name-prefix filtering, got prefix %q", got)
	}

	if got := client.requests[0].GetPageToken(); got != "" {
		t.Fatalf("expected first page token empty, got %q", got)
	}

	if got := client.requests[1].GetPageToken(); got != "1" {
		t.Fatalf("expected second page token %q, got %q", "1", got)
	}
}

func TestResolveDatasetSelectorID_WithName(t *testing.T) {
	client := &mockIDResolverClient{}

	selector := &storagepb.DatasetSelector{
		Selector: &storagepb.DatasetSelector_DatasetName{
			DatasetName: &storagepb.DatasetNameSelector{
				Name: "my-dataset",
			},
		},
	}

	result, err := resolveDatasetSelectorID(context.Background(), client, selector)

	if err != nil {
		t.Fatalf("Expected no error for dataset name selector, got: %v", err)
	}

	if result.GetDatasetName() == nil {
		t.Error("Expected dataset name selector to be preserved")
	}
}

func TestResolveDatasetSelectorID_WithShortID(t *testing.T) {
	client := &mockIDResolverClient{
		datasets: []*storagepb.DatasetSummary{
			{DatasetId: "abcdef1234567890abcdef1234567890"},
		},
	}

	selector := &storagepb.DatasetSelector{
		Selector: &storagepb.DatasetSelector_DatasetId{
			DatasetId: "abcd",
		},
	}

	result, err := resolveDatasetSelectorID(context.Background(), client, selector)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if result.GetDatasetId() != "abcdef1234567890abcdef1234567890" {
		t.Errorf("Expected full ID, got: %s", result.GetDatasetId())
	}
}
