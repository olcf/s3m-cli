package storagecmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"
	"golang.org/x/term"

	"github.com/olcf/s3m-cli/internal/output"
	"github.com/olcf/s3m-cli/internal/runtime"
	"github.com/olcf/s3m-cli/internal/util"

	storagepb "github.com/olcf/s3m-apis/storage/v1alpha"
)

func buildDeleteCommand(rt *runtime.Runtime) *cli.Command {
	return &cli.Command{
		Name:      "delete",
		Usage:     "Delete a dataset",
		ArgsUsage: "<dataset-id>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Value:   "text",
				Usage:   "Output format: text, json",
			},
			&cli.BoolFlag{
				Name:    "yes",
				Aliases: []string{"y"},
				Usage:   "Skip confirmation prompt",
			},
			&cli.StringFlag{
				Name:  "token",
				Usage: tokenFlagUsage,
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			return runDelete(ctx, c, rt)
		},
	}
}

func isInteractive() bool {
	fd, err := util.FileDescriptor(os.Stdin)
	if err != nil {
		return false
	}

	return term.IsTerminal(fd)
}

func runDelete(ctx context.Context, c *cli.Command, rt *runtime.Runtime) error {
	if c.NArg() != 1 {
		return errors.New("usage: s3m storage delete <dataset-id>")
	}

	datasetIDOrPrefix := c.Args().Get(0)
	format := output.Format(c.String("output"))
	out := output.NewOutput(format, os.Stdout)

	return withClient(ctx, c, rt, runtime.StorageOpDelete, func(client storagepb.BucketGatewayClient, _ string) error {
		datasetID, err := resolveDatasetID(ctx, client, datasetIDOrPrefix)
		if err != nil {
			return err
		}

		if !c.Bool("yes") && isInteractive() {
			fmt.Fprintf(os.Stderr, "Delete dataset %s? [y/N] ", datasetID)

			reader := bufio.NewReader(os.Stdin)

			answer, _ := reader.ReadString('\n')
			if strings.TrimSpace(strings.ToLower(answer)) != "y" {
				return errors.New("aborted")
			}
		}

		if _, err := client.DeleteDataset(ctx, &storagepb.DeleteDatasetRequest{DatasetId: datasetID}); err != nil {
			return fmt.Errorf("delete dataset: %w", err)
		}

		out.Success(fmt.Sprintf("Dataset %s deleted", datasetID))
		out.AddField("datasetId", datasetID)
		out.AddField("deleted", true)

		return out.Render()
	})
}
