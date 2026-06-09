package toolset

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	storagepb "github.com/olcf/s3m-apis/storage/v1alpha"
)

//
// Handler logic

func (h *storageHandlers) handleListDatasets(
	ctx context.Context, input listDatasetsRequest,
) (*listDatasetsResponse, error) {
	client := storagepb.NewBucketGatewayClient(h.conn)

	callCtx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	resp, err := client.ListDatasets(callCtx, &storagepb.ListDatasetsRequest{
		Prefix:    input.Prefix,
		PageSize:  safeUint32(input.PageSize),
		PageToken: input.PageToken,
	})
	if err != nil {
		return nil, fmt.Errorf("list datasets: %w", err)
	}

	datasets := make([]datasetSummary, 0, len(resp.GetDatasets()))

	for _, ds := range resp.GetDatasets() {
		summary := datasetSummary{
			DatasetID:     ds.GetDatasetId(),
			DatasetName:   ds.GetDatasetName(),
			State:         datasetStateToString(ds.GetState()),
			ContentLength: ds.GetContentLength(),
		}

		if ds.GetCreatedAt() != nil {
			summary.CreatedAt = ds.GetCreatedAt().AsTime().Format(time.RFC3339)
		}

		if ds.GetExpiresAt() != nil {
			summary.ExpiresAt = ds.GetExpiresAt().AsTime().Format(time.RFC3339)
		}

		datasets = append(datasets, summary)
	}

	result := &listDatasetsResponse{
		Datasets: datasets,
	}

	if resp.GetPagination() != nil {
		result.HasMore = resp.GetPagination().GetHasMore()
		result.NextOffset = int(resp.GetPagination().GetNextOffset())
		result.PageToken = resp.GetPagination().GetNextPageToken()
	}

	return result, nil
}

func (h *storageHandlers) handleListFiles(
	ctx context.Context, input listFilesRequest,
) (*listFilesResponse, error) {
	selector, err := buildSelector(input.DatasetID, input.DatasetName, input.Latest)
	if err != nil {
		return nil, err
	}

	client := storagepb.NewBucketGatewayClient(h.conn)

	callCtx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	resp, err := client.GetDatasetContents(callCtx, &storagepb.GetDatasetContentsRequest{
		Selector:    selector,
		PathPrefix:  input.PathPrefix,
		GlobPattern: input.GlobPattern,
		PageSize:    safeUint32(input.PageSize),
		PageToken:   input.PageToken,
	})
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}

	files := make([]fileEntry, 0, len(resp.GetFiles()))

	for _, f := range resp.GetFiles() {
		entry := fileEntry{
			Path: f.GetPath(),
			Size: f.GetSize(),
		}

		if f.GetLastModified() != nil {
			entry.LastModified = f.GetLastModified().AsTime().Format(time.RFC3339)
		}

		files = append(files, entry)
	}

	result := &listFilesResponse{
		Files: files,
	}

	if resp.GetPagination() != nil {
		result.HasMore = resp.GetPagination().GetHasMore()
		result.NextOffset = int(resp.GetPagination().GetNextOffset())
		result.PageToken = resp.GetPagination().GetNextPageToken()
	}

	return result, nil
}

//nolint:funlen // readFile has multiple validation steps and HTTP fetching
func (h *storageHandlers) handleReadFile(
	ctx context.Context, input readFileRequest,
) (*readFileResponse, error) {
	if strings.TrimSpace(input.Path) == "" {
		return nil, errors.New("path is required")
	}

	selector, err := buildSelector(input.DatasetID, input.DatasetName, input.Latest)
	if err != nil {
		return nil, err
	}

	// Apply defaults
	length := input.Length
	if length <= 0 {
		length = defaultReadLength
	}

	if length > maxReadLength {
		length = maxReadLength
	}

	// Get presigned URL
	client := storagepb.NewBucketGatewayClient(h.conn)

	callCtx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	urlResp, err := client.GetDownloadURLs(callCtx, &storagepb.GetDownloadURLsRequest{
		Selector: selector,
		PathFilter: &storagepb.GetDownloadURLsRequest_Paths{
			Paths: &storagepb.PathList{
				Paths: []string{input.Path},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("get download URL: %w", err)
	}

	if len(urlResp.GetDownloads()) == 0 {
		return nil, fmt.Errorf("file not found: %s", input.Path)
	}

	downloadURL := urlResp.GetDownloads()[0].GetDownloadUrl()

	// Fetch content with range header
	content, totalSize, err := h.fetchContentRange(ctx, downloadURL, input.Offset, length)
	if err != nil {
		return nil, fmt.Errorf("fetch content: %w", err)
	}

	// Determine if content is valid UTF-8 text
	contentType := "text"

	contentStr := string(content)
	if !isValidUTF8Text(content) {
		contentType = "base64"
		contentStr = base64.StdEncoding.EncodeToString(content)
	}

	return &readFileResponse{
		Content:     contentStr,
		ContentType: contentType,
		TotalSize:   totalSize,
		Offset:      input.Offset,
		Length:      len(content),
		HasMore:     input.Offset+int64(len(content)) < totalSize,
	}, nil
}

func (h *storageHandlers) handleGetDownloadURL(
	ctx context.Context, input getDownloadURLRequest,
) (*getDownloadURLResponse, error) {
	if strings.TrimSpace(input.Path) == "" {
		return nil, errors.New("path is required")
	}

	selector, err := buildSelector(input.DatasetID, input.DatasetName, input.Latest)
	if err != nil {
		return nil, err
	}

	client := storagepb.NewBucketGatewayClient(h.conn)

	// Reserve the dataset
	callCtx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	resp, err := client.GetDownloadURLs(callCtx, &storagepb.GetDownloadURLsRequest{
		Selector: selector,
		PathFilter: &storagepb.GetDownloadURLsRequest_Paths{
			Paths: &storagepb.PathList{
				Paths: []string{input.Path},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("get download URL: %w", err)
	}

	if len(resp.GetDownloads()) == 0 {
		return nil, fmt.Errorf("file not found: %s", input.Path)
	}

	result := &getDownloadURLResponse{
		URL: resp.GetDownloads()[0].GetDownloadUrl(),
	}

	if resp.GetExpiresAt() != nil {
		result.ExpiresAt = resp.GetExpiresAt().AsTime().Format(time.RFC3339)
	}

	return result, nil
}

func (h *storageHandlers) handlePutFile(
	ctx context.Context, input putFileRequest,
) (*putFileResponse, error) {
	if strings.TrimSpace(input.DatasetName) == "" {
		return nil, errors.New("dataset_name is required")
	}

	if strings.TrimSpace(input.Path) == "" {
		return nil, errors.New("path is required")
	}

	if input.Content == "" {
		return nil, errors.New("content is required")
	}

	client := storagepb.NewBucketGatewayClient(h.conn)

	callCtx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	reserveResp, err := client.ReserveDataset(callCtx, &storagepb.ReserveDatasetRequest{
		DatasetName: input.DatasetName,
		Paths:       []string{input.Path},
	})
	if err != nil {
		return nil, fmt.Errorf("reserve dataset: %w", err)
	}

	if len(reserveResp.GetUploads()) == 0 {
		return nil, errors.New("no upload URL returned")
	}

	uploadURL := reserveResp.GetUploads()[0].GetUploadUrl()
	contentBytes := []byte(input.Content)

	// Upload content via HTTP PUT
	if err := h.uploadContent(ctx, uploadURL, contentBytes); err != nil {
		h.cleanupReservedDataset(ctx, client, reserveResp.GetDatasetId())

		return nil, fmt.Errorf("upload content: %w", err)
	}

	// Commit the dataset
	commitCtx, commitCancel := context.WithTimeout(ctx, h.timeout)
	defer commitCancel()

	_, err = client.CommitDataset(commitCtx, &storagepb.CommitDatasetRequest{
		DatasetId: reserveResp.GetDatasetId(),
	})
	if err != nil {
		return nil, fmt.Errorf("commit dataset: %w", err)
	}

	return &putFileResponse{
		DatasetID:   reserveResp.GetDatasetId(),
		DatasetName: reserveResp.GetDatasetName(),
		Path:        input.Path,
		Size:        len(contentBytes),
	}, nil
}

func (h *storageHandlers) handleDeleteDataset(
	ctx context.Context, input deleteDatasetRequest,
) (*deleteDatasetResponse, error) {
	if strings.TrimSpace(input.DatasetID) == "" {
		return nil, errors.New("dataset_id is required")
	}

	client := storagepb.NewBucketGatewayClient(h.conn)

	callCtx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	_, err := client.DeleteDataset(callCtx, &storagepb.DeleteDatasetRequest{
		DatasetId: input.DatasetID,
	})
	if err != nil {
		return nil, fmt.Errorf("delete dataset: %w", err)
	}

	return &deleteDatasetResponse{
		Success: true,
	}, nil
}
