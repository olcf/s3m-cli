//nolint:tagliatelle // API uses snake_case
package toolset

import (
	"time"

	"google.golang.org/grpc"
)

//
// Constants

const (
	defaultReadLength = 250
	maxReadLength     = 2000
)

//
// Handler struct

type storageHandlers struct {
	conn    *grpc.ClientConn
	timeout time.Duration
	debug   bool
}

//
// Request types

type listDatasetsRequest struct {
	Prefix    string `json:"prefix,omitempty"`
	PageSize  int    `json:"page_size,omitempty"`
	PageToken string `json:"page_token,omitempty"`
}

type listFilesRequest struct {
	DatasetID   string `json:"dataset_id,omitempty"`
	DatasetName string `json:"dataset_name,omitempty"`
	Latest      bool   `json:"latest,omitempty"`
	GlobPattern string `json:"glob_pattern,omitempty"`
	PathPrefix  string `json:"path_prefix,omitempty"`
	PageSize    int    `json:"page_size,omitempty"`
	PageToken   string `json:"page_token,omitempty"`
}

type readFileRequest struct {
	DatasetID   string `json:"dataset_id,omitempty"`
	DatasetName string `json:"dataset_name,omitempty"`
	Latest      bool   `json:"latest,omitempty"`
	Path        string `json:"path"`
	Offset      int64  `json:"offset,omitempty"`
	Length      int    `json:"length,omitempty"`
}

type getDownloadURLRequest struct {
	DatasetID   string `json:"dataset_id,omitempty"`
	DatasetName string `json:"dataset_name,omitempty"`
	Latest      bool   `json:"latest,omitempty"`
	Path        string `json:"path"`
}

type putFileRequest struct {
	DatasetName string `json:"dataset_name"`
	Path        string `json:"path"`
	Content     string `json:"content"`
}

type deleteDatasetRequest struct {
	DatasetID string `json:"dataset_id"`
}

//
// Response types

type listDatasetsResponse struct {
	Datasets   []datasetSummary `json:"datasets"`
	HasMore    bool             `json:"has_more"`
	NextOffset int              `json:"next_offset,omitempty"`
	PageToken  string           `json:"page_token,omitempty"`
}

type datasetSummary struct {
	DatasetID     string `json:"dataset_id"`
	DatasetName   string `json:"dataset_name"`
	State         string `json:"state"`
	CreatedAt     string `json:"created_at,omitempty"`
	ExpiresAt     string `json:"expires_at,omitempty"`
	ContentLength int64  `json:"content_length"`
}

type listFilesResponse struct {
	Files      []fileEntry `json:"files"`
	HasMore    bool        `json:"has_more"`
	NextOffset int         `json:"next_offset,omitempty"`
	PageToken  string      `json:"page_token,omitempty"`
}

type fileEntry struct {
	Path         string `json:"path"`
	Size         int64  `json:"size"`
	LastModified string `json:"last_modified,omitempty"`
}

type readFileResponse struct {
	Content     string `json:"content"`
	ContentType string `json:"content_type"` // "text" or "base64"
	TotalSize   int64  `json:"total_size"`
	Offset      int64  `json:"offset"`
	Length      int    `json:"length"`
	HasMore     bool   `json:"has_more"`
}

type getDownloadURLResponse struct {
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

type putFileResponse struct {
	DatasetID   string `json:"dataset_id"`
	DatasetName string `json:"dataset_name"`
	Path        string `json:"path"`
	Size        int    `json:"size"`
}

type deleteDatasetResponse struct {
	Success bool `json:"success"`
}
