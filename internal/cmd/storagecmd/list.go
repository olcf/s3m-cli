package storagecmd

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/olcf/s3m-cli/internal/output"
	"github.com/olcf/s3m-cli/internal/runtime"

	storagepb "github.com/olcf/s3m-apis/storage/v1alpha"
)

func buildLsCommand(rt *runtime.Runtime) *cli.Command {
	return &cli.Command{
		Name:      "ls",
		Usage:     "List datasets or files within a dataset",
		ArgsUsage: "[<dataset> [<pattern>]]",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "full-id",
				Usage: "Show full dataset IDs when listing datasets",
			},
			&cli.BoolFlag{
				Name:  "id",
				Usage: "Treat dataset argument as a dataset ID",
			},
			&cli.BoolFlag{
				Name:  "latest",
				Usage: "Select the latest READY dataset when using a dataset name",
			},
			&cli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Value:   "text",
				Usage:   "Output format: text, json",
			},
			&cli.StringFlag{
				Name:  "token",
				Usage: tokenFlagUsage,
			},
			&cli.IntFlag{
				Name:    "limit",
				Aliases: []string{"l"},
				Value:   0,
				Usage:   "Maximum number of items to display (0 for API default)",
			},
			&cli.IntFlag{
				Name:  "offset",
				Value: 0,
				Usage: "Number of items to skip before displaying",
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			return runLs(ctx, c, rt)
		},
	}
}

func runLs(ctx context.Context, c *cli.Command, rt *runtime.Runtime) error {
	return withClient(ctx, c, rt, runtime.StorageOpRead, func(client storagepb.BucketGatewayClient, _ string) error {
		return runLsWithClient(ctx, c, client)
	})
}

func runLsWithClient(ctx context.Context, c *cli.Command, client storagepb.BucketGatewayClient) error {
	if c.NArg() > 2 {
		return errors.New("usage: s3m storage ls [<dataset> [<pattern>]]")
	}

	if c.NArg() == 0 {
		return listDatasets(ctx, client, c)
	}

	dataset := c.Args().Get(0)
	pattern := ""

	if c.NArg() == 2 {
		pattern = c.Args().Get(1)
	}

	selector, err := buildDatasetSelector(dataset, c.Bool("id"), c.Bool("latest"))
	if err != nil {
		return err
	}

	selector, err = resolveDatasetSelectorID(ctx, client, selector)
	if err != nil {
		return err
	}

	return listDatasetContents(ctx, client, selector, dataset, pattern, c)
}

func listDatasets(ctx context.Context, client storagepb.BucketGatewayClient, c *cli.Command) error {
	req := buildListDatasetsRequest(c.Int("limit"), c.Int("offset"))

	resp, err := client.ListDatasets(ctx, req)
	if err != nil {
		return fmt.Errorf("list datasets: %w", err)
	}

	format := output.Format(c.String("output"))
	out := output.NewOutput(format, os.Stdout)

	if len(resp.GetDatasets()) == 0 {
		out.Infof("No datasets found")
		return out.Render()
	}

	if err := out.SetProtoMessageList(resp, "datasets"); err != nil {
		return err
	}

	addPagination(out, c.Int("limit"), c.Int("offset"), len(resp.GetDatasets()), resp.GetPagination())

	out.SetTable(buildDatasetTable(resp.GetDatasets(), c.Bool("full-id")))

	return out.Render()
}

func listDatasetContents(
	ctx context.Context,
	client storagepb.BucketGatewayClient,
	selector *storagepb.DatasetSelector,
	dataset, pattern string,
	c *cli.Command,
) error {
	req := &storagepb.GetDatasetContentsRequest{
		Selector: selector,
	}

	if isGlobPattern(pattern) {
		req.GlobPattern = pattern
	} else if pattern != "" {
		req.PathPrefix = pattern
	}

	if limit := c.Int("limit"); limit > 0 {
		req.PageSize = safeUint32(limit)
	}

	if offset := c.Int("offset"); offset > 0 {
		req.Offset = safeUint32(offset)
	}

	resp, err := client.GetDatasetContents(ctx, req)
	if err != nil {
		return fmt.Errorf("get dataset contents: %w", err)
	}

	files := resp.GetFiles()

	format := output.Format(c.String("output"))
	out := output.NewOutput(format, os.Stdout)

	if len(files) == 0 {
		if pattern != "" {
			out.Infof("No files found in dataset %s matching %s", dataset, pattern)
		} else {
			out.Infof("Dataset %s is empty", dataset)
		}

		return out.Render()
	}

	if err := out.SetProtoMessageList(resp, "files"); err != nil {
		return err
	}

	addPagination(out, c.Int("limit"), c.Int("offset"), len(files), resp.GetPagination())

	return buildFileTable(out, files)
}

func buildFileTable(out *output.Output, files []*storagepb.FileEntry) error {
	table := output.TableConfig{
		Headers: []string{"PATH", "SIZE", "MODIFIED"},
		Rows:    make([][]string, len(files)),
		ColumnConfigs: []output.ColumnConfig{
			{Name: "PATH", MaxWidth: 60, Truncate: output.TruncateModeMiddle},
			{Name: "SIZE", MaxWidth: 12, Align: output.AlignRight},
			{Name: "MODIFIED", MaxWidth: 20},
		},
	}

	for i, f := range files {
		table.Rows[i] = []string{
			f.GetPath(),
			output.FormatBytes(f.GetSize()),
			output.FormatTimestamp(f.GetLastModified().AsTime()),
		}
	}

	out.SetTable(table)

	return out.Render()
}

func addPagination(out *output.Output, limit, offset, returned int, pagination *storagepb.Pagination) {
	var (
		nextOffset    uint32
		hasMore       bool
		nextPageToken string
	)

	if pagination != nil {
		nextOffset = pagination.GetNextOffset()
		hasMore = pagination.GetHasMore()
		nextPageToken = pagination.GetNextPageToken()
	}

	paginationOutput := output.Pagination{
		Limit:         safeUint32(limit),
		Offset:        safeUint32(offset),
		Returned:      safeUint32(returned),
		NextOffset:    nextOffset,
		HasMore:       hasMore,
		NextPageToken: nextPageToken,
	}

	out.SetPagination(paginationOutput)
}

func buildListDatasetsRequest(limit, offset int) *storagepb.ListDatasetsRequest {
	req := &storagepb.ListDatasetsRequest{}

	if limit > 0 {
		req.PageSize = safeUint32(limit)
	}

	if offset > 0 {
		req.Offset = safeUint32(offset)
	}

	return req
}

func buildDatasetTable(datasets []*storagepb.DatasetSummary, fullID bool) output.TableConfig {
	table := output.TableConfig{
		Headers: []string{"DATASET", "ID", "STATE", "CREATED", "EXPIRES", "SIZE"},
		Rows:    make([][]string, len(datasets)),
		ColumnConfigs: []output.ColumnConfig{
			{Name: "DATASET", MaxWidth: 40, Truncate: output.TruncateModeMiddle},
			{Name: "ID", MaxWidth: 12, Truncate: output.TruncateModeEnd},
			{Name: "STATE", MaxWidth: 12},
			{Name: "CREATED", MaxWidth: 20},
			{Name: "EXPIRES", MaxWidth: 20},
			{Name: "SIZE", Align: output.AlignRight},
		},
	}

	for i, ds := range datasets {
		id := ds.GetDatasetId()
		if !fullID {
			id = output.ShortID(id)
		}

		state := strings.TrimPrefix(ds.GetState().String(), "DATASET_STATE_")

		table.Rows[i] = []string{
			ds.GetDatasetName(),
			id,
			state,
			output.FormatTimestamp(ds.GetCreatedAt().AsTime()),
			output.FormatTimestamp(ds.GetExpiresAt().AsTime()),
			output.FormatBytes(ds.GetContentLength()),
		}
	}

	return table
}

func safeUint32(value int) uint32 {
	if value <= 0 {
		return 0
	}

	if value > math.MaxUint32 {
		return math.MaxUint32
	}

	return uint32(value)
}
