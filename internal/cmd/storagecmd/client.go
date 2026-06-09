package storagecmd

import (
	"context"
	"errors"
	"log/slog"

	"github.com/urfave/cli/v3"
	"google.golang.org/grpc"

	"github.com/olcf/s3m-cli/internal/runtime"

	storagepb "github.com/olcf/s3m-apis/storage/v1alpha"
)

const tokenFlagUsage = "Override auth token (for scoped compute job tokens)" // #nosec G101

//
// Client helpers

func withClient(
	ctx context.Context, c *cli.Command, rt *runtime.Runtime, op runtime.StorageOp,
	fn func(storagepb.BucketGatewayClient, string) error,
) error {
	token, err := resolveToken(c, rt, op)
	if err != nil {
		return err
	}

	conn, err := rt.Dial(ctx, token)
	if err != nil {
		return err
	}
	defer closeConn(conn)

	return fn(storagepb.NewBucketGatewayClient(conn), token)
}

func resolveToken(c *cli.Command, rt *runtime.Runtime, op runtime.StorageOp) (string, error) {
	// Explicit --token flag always wins.
	if token := c.String("token"); token != "" {
		return token, nil
	}

	// S3MIO environment token (set by compute-utils for Slurm jobs).
	if token, ok := rt.StorageToken(op); ok {
		return token, nil
	}

	// Fall back to the primary auth state token.
	if err := rt.EnsureState(); err != nil {
		return "", err
	}

	if rec, ok := rt.CurrentToken(); ok {
		return rec.Token, nil
	}

	return "", errors.New("no auth token available; run `s3m login token` or pass `--token`")
}

func closeConn(conn *grpc.ClientConn) {
	if err := conn.Close(); err != nil {
		slog.Debug("failed to close connection", "error", err)
	}
}

func buildDatasetSelector(dataset string, useID, latest bool) (*storagepb.DatasetSelector, error) {
	if dataset == "" {
		return nil, errors.New("dataset is required")
	}

	if useID {
		if latest {
			return nil, errors.New("cannot use --latest with --id")
		}

		// Dataset is stored as-is here; resolveDatasetSelectorID resolves short IDs later
		return &storagepb.DatasetSelector{
			Selector: &storagepb.DatasetSelector_DatasetId{
				DatasetId: dataset,
			},
		}, nil
	}

	return &storagepb.DatasetSelector{
		Selector: &storagepb.DatasetSelector_DatasetName{
			DatasetName: &storagepb.DatasetNameSelector{
				Name:   dataset,
				Latest: latest,
			},
		},
	}, nil
}

//
// Pagination helpers

func listAllFiles(
	ctx context.Context,
	client storagepb.BucketGatewayClient,
	selector *storagepb.DatasetSelector,
	pattern string,
) ([]*storagepb.FileEntry, error) {
	var files []*storagepb.FileEntry

	req := &storagepb.GetDatasetContentsRequest{
		Selector: selector,
	}

	if isGlobPattern(pattern) {
		req.GlobPattern = pattern
	} else if pattern != "" {
		req.PathPrefix = pattern
	}

	pageToken := ""
	for {
		req.PageToken = pageToken

		resp, err := client.GetDatasetContents(ctx, req)
		if err != nil {
			return nil, err
		}

		files = append(files, resp.GetFiles()...)

		pageToken = resp.GetPagination().GetNextPageToken()
		if pageToken == "" {
			break
		}
	}

	return files, nil
}
