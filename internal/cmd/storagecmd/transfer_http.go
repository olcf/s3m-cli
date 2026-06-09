package storagecmd

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/olcf/s3m-cli/internal/cmd"

	storagepb "github.com/olcf/s3m-apis/storage/v1alpha"
)

const transferResponseHeaderTimeout = 5 * time.Minute

func newTransferHTTPClient(responseHeaderTimeout time.Duration) *http.Client {
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Client{}
	}

	transport := defaultTransport.Clone()
	transport.ResponseHeaderTimeout = responseHeaderTimeout

	return &http.Client{Transport: transport}
}

func cleanupReservedDataset(ctx context.Context, client storagepb.BucketGatewayClient, datasetID string) {
	cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), cmd.GRPCCallTimeout)
	defer cleanupCancel()

	if _, err := client.DeleteDataset(cleanupCtx, &storagepb.DeleteDatasetRequest{DatasetId: datasetID}); err != nil {
		slog.Debug("failed to clean up reserved dataset", "datasetId", datasetID, "error", err)
	}
}
