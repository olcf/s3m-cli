package storagecmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	storagepb "github.com/olcf/s3m-apis/storage/v1alpha"
)

const (
	fullIDLength = 32
)

// resolveDatasetID resolves a short or full dataset ID to a full 32-character ID.
// Accepts:
//   - Full 32-char ID: returns as-is
//   - Short ID (1-31 chars): searches for matching dataset by prefix
//
// Returns an error if:
//   - No dataset matches the prefix
//   - Multiple datasets match the prefix (ambiguous)
func resolveDatasetID(ctx context.Context, client storagepb.BucketGatewayClient, idOrPrefix string) (string, error) {
	if len(idOrPrefix) == fullIDLength {
		return idOrPrefix, nil
	}

	var matches []string

	pageToken := ""
	for {
		resp, err := client.ListDatasets(ctx, &storagepb.ListDatasetsRequest{
			PageToken: pageToken,
		})
		if err != nil {
			return "", fmt.Errorf("list datasets to resolve ID: %w", err)
		}

		for _, ds := range resp.GetDatasets() {
			if strings.HasPrefix(ds.GetDatasetId(), idOrPrefix) {
				matches = append(matches, ds.GetDatasetId())

				if len(matches) > 1 {
					return "", fmt.Errorf(
						"ambiguous dataset ID prefix %q matches multiple datasets (specify more characters)",
						idOrPrefix,
					)
				}
			}
		}

		pageToken = resp.GetPagination().GetNextPageToken()
		if pageToken == "" {
			break
		}
	}

	if len(matches) == 0 {
		return "", fmt.Errorf("no dataset found with ID prefix: %s", idOrPrefix)
	}

	return matches[0], nil
}

// resolveDatasetSelectorID resolves a dataset selector's ID if it uses DatasetId.
// If the selector uses DatasetName, it's returned as-is.
// If the selector uses DatasetId with a short prefix, it's resolved to the full ID.
func resolveDatasetSelectorID(
	ctx context.Context,
	client storagepb.BucketGatewayClient,
	selector *storagepb.DatasetSelector,
) (*storagepb.DatasetSelector, error) {
	if selector == nil {
		return nil, errors.New("selector is nil")
	}

	if selector.GetDatasetName() != nil {
		return selector, nil
	}

	idOrPrefix := selector.GetDatasetId()
	if idOrPrefix == "" {
		return nil, errors.New("dataset ID is empty")
	}

	fullID, err := resolveDatasetID(ctx, client, idOrPrefix)
	if err != nil {
		return nil, err
	}

	return &storagepb.DatasetSelector{
		Selector: &storagepb.DatasetSelector_DatasetId{
			DatasetId: fullID,
		},
	}, nil
}
