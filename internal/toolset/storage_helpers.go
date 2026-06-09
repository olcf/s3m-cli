package toolset

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"unicode/utf8"

	storagepb "github.com/olcf/s3m-apis/storage/v1alpha"

	grpcclient "github.com/olcf/s3m-cli/internal/grpc"
)

//
// Helper functions

func safeUint32(value int) uint32 {
	if value <= 0 {
		return 0
	}

	if value > math.MaxUint32 {
		return math.MaxUint32
	}

	return uint32(value)
}

func buildSelector(datasetID, datasetName string, latest bool) (*storagepb.DatasetSelector, error) {
	datasetID = strings.TrimSpace(datasetID)
	datasetName = strings.TrimSpace(datasetName)

	if datasetID == "" && datasetName == "" {
		return nil, errors.New("provide dataset_id or dataset_name")
	}

	if datasetID != "" {
		return &storagepb.DatasetSelector{
			Selector: &storagepb.DatasetSelector_DatasetId{
				DatasetId: datasetID,
			},
		}, nil
	}

	return &storagepb.DatasetSelector{
		Selector: &storagepb.DatasetSelector_DatasetName{
			DatasetName: &storagepb.DatasetNameSelector{
				Name:   datasetName,
				Latest: latest,
			},
		},
	}, nil
}

func isValidUTF8Text(data []byte) bool {
	if !utf8.Valid(data) {
		return false
	}

	// Check for common binary indicators (null bytes, etc.)
	for _, b := range data {
		if b == 0 {
			return false
		}

		// Allow common control characters (tab, newline, carriage return)
		if b < 32 && b != '\t' && b != '\n' && b != '\r' {
			return false
		}
	}

	return true
}

func datasetStateToString(state storagepb.DatasetState) string {
	switch state {
	case storagepb.DatasetState_DATASET_STATE_UNSPECIFIED:
		return "unspecified"
	case storagepb.DatasetState_DATASET_STATE_UPLOADING:
		return "uploading"
	case storagepb.DatasetState_DATASET_STATE_READY:
		return "ready"
	case storagepb.DatasetState_DATASET_STATE_REMOVED:
		return "removed"
	}

	return "unspecified"
}

func (h *storageHandlers) fetchContentRange(
	ctx context.Context, url string, offset int64, length int,
) ([]byte, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}

	// Set range header
	endByte := offset + int64(length) - 1
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, endByte))

	// Add auth token if present in context (for stateless mode)
	if token, ok := grpcclient.AuthTokenFromContext(ctx); ok {
		req.Header.Set("Authorization", token)
	}

	client := &http.Client{Timeout: h.timeout}

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	totalSize := responseTotalSize(resp)

	if resp.StatusCode == http.StatusOK {
		if offset > 0 {
			return nil, 0, errors.New("storage backend ignored byte range request")
		}

		content, err := io.ReadAll(io.LimitReader(resp.Body, int64(length)+1))
		if err != nil {
			return nil, 0, err
		}

		if len(content) > length {
			return nil, 0, errors.New("storage backend ignored byte range request")
		}

		if totalSize == 0 {
			totalSize = int64(len(content))
		}

		return content, totalSize, nil
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}

	return content, totalSize, nil
}

func responseTotalSize(resp *http.Response) int64 {
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		var totalSize int64

		if idx := strings.LastIndex(cr, "/"); idx != -1 {
			_, _ = fmt.Sscanf(cr[idx+1:], "%d", &totalSize)
		}

		if totalSize != 0 {
			return totalSize
		}
	}

	return resp.ContentLength
}

func (h *storageHandlers) uploadContent(ctx context.Context, url string, content []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(content))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(len(content))

	// Add auth token if present in context (for stateless mode)
	if token, ok := grpcclient.AuthTokenFromContext(ctx); ok {
		req.Header.Set("Authorization", token)
	}

	client := &http.Client{Timeout: h.timeout}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (h *storageHandlers) cleanupReservedDataset(
	ctx context.Context, client storagepb.BucketGatewayClient, datasetID string,
) {
	cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), h.timeout)
	defer cleanupCancel()

	if _, err := client.DeleteDataset(cleanupCtx, &storagepb.DeleteDatasetRequest{DatasetId: datasetID}); err != nil {
		slog.Debug("failed to clean up reserved dataset", "datasetId", datasetID, "error", err)
	}
}
