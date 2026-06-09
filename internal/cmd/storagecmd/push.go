package storagecmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/olcf/s3m-cli/internal/output"
	"github.com/olcf/s3m-cli/internal/runtime"

	storagepb "github.com/olcf/s3m-apis/storage/v1alpha"
)

func buildPushCommand(rt *runtime.Runtime) *cli.Command {
	return &cli.Command{
		Name:      "push",
		Usage:     "Upload local files to a dataset",
		ArgsUsage: "<dataset> <local-path> [<remote-path>]",
		Flags: []cli.Flag{
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
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			return runPush(ctx, c, rt)
		},
	}
}

func runPush(ctx context.Context, c *cli.Command, rt *runtime.Runtime) error {
	if c.NArg() < 2 || c.NArg() > 3 {
		return errors.New("usage: s3m storage push <dataset> <local-path> [<remote-path>]")
	}

	dataset := c.Args().Get(0)
	localPath := c.Args().Get(1)
	remotePath := ""

	if c.NArg() == 3 {
		remotePath = c.Args().Get(2)
	}

	files, err := collectLocalFiles(localPath, remotePath)
	if err != nil {
		return err
	}

	if len(files) == 0 {
		return errors.New("no files to upload")
	}

	return withClient(ctx, c, rt, runtime.StorageOpWrite, func(client storagepb.BucketGatewayClient, token string) error {
		return reserveAndUpload(ctx, client, dataset, files, token, c)
	})
}

//
// Upload workflow

func reserveAndUpload(
	ctx context.Context,
	client storagepb.BucketGatewayClient,
	dataset string,
	files map[string]string,
	token string,
	c *cli.Command,
) error {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}

	format := output.Format(c.String("output"))
	out := output.NewOutput(format, os.Stdout)

	out.Infof("Reserving dataset %s with %d file(s)...", dataset, len(files))

	resp, err := client.ReserveDataset(ctx, &storagepb.ReserveDatasetRequest{
		DatasetName: dataset,
		Paths:       paths,
	})
	if err != nil {
		return fmt.Errorf("reserve dataset: %w", err)
	}

	totalBytes, err := uploadAll(ctx, client, resp, files, token, out)
	if err != nil {
		return err
	}

	out.Infof("Committing dataset...")

	if _, err := client.CommitDataset(ctx, &storagepb.CommitDatasetRequest{DatasetId: resp.GetDatasetId()}); err != nil {
		return fmt.Errorf("commit dataset: %w", err)
	}

	out.Success(fmt.Sprintf("Dataset %s uploaded (id %s)", dataset, resp.GetDatasetId()))
	out.AddField("datasetName", dataset)
	out.AddField("datasetId", resp.GetDatasetId())
	out.AddField("filesUploaded", len(files))
	out.AddField("totalBytes", totalBytes)

	return out.Render()
}

func uploadAll(
	ctx context.Context,
	client storagepb.BucketGatewayClient,
	resp *storagepb.ReserveDatasetResponse,
	files map[string]string,
	token string,
	out *output.Output,
) (int64, error) {
	httpClient := newTransferHTTPClient(transferResponseHeaderTimeout)

	var totalBytes int64

	for _, target := range resp.GetUploads() {
		p := target.GetPath()
		localFile := files[p]

		out.Infof("Uploading %s...", p)

		if err := uploadFile(ctx, httpClient, localFile, target.GetUploadUrl(), token); err != nil {
			// Best-effort cleanup of the reserved dataset
			cleanupReservedDataset(ctx, client, resp.GetDatasetId())
			return 0, fmt.Errorf("upload %s: %w", p, err)
		}

		if stat, err := os.Stat(localFile); err == nil {
			totalBytes += stat.Size()
		}
	}

	return totalBytes, nil
}

//
// File collection

//nolint:nestif
func collectLocalFiles(localPath, remotePath string) (map[string]string, error) {
	files := make(map[string]string)

	info, err := os.Stat(localPath)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", localPath, err)
	}

	if info.IsDir() {
		err = filepath.WalkDir(localPath, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}

			if d.Type()&os.ModeSymlink != 0 {
				slog.Debug("skipping symlink", "path", path)
				return nil
			}

			relPath, relErr := filepath.Rel(localPath, path)
			if relErr != nil {
				return fmt.Errorf("rel %s: %w", path, relErr)
			}

			relPath = filepath.ToSlash(relPath)

			if remotePath != "" {
				relPath = strings.TrimRight(remotePath, "/") + "/" + relPath
			}

			files[relPath] = path

			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk %s: %w", localPath, err)
		}

		return files, nil
	}

	remoteFile := remotePath
	if remoteFile == "" {
		remoteFile = filepath.Base(localPath)
	} else if strings.HasSuffix(remoteFile, "/") {
		remoteFile += filepath.Base(localPath)
	}

	files[remoteFile] = localPath

	return files, nil
}

//
// File operations

func uploadFile(ctx context.Context, client *http.Client, localPath, uploadURL, token string) error {
	f, err := os.Open(localPath) // #nosec G304
	if err != nil {
		return err
	}

	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	if slog.Default().Enabled(ctx, slog.LevelDebug) {
		if parsed, parseErr := url.Parse(uploadURL); parseErr == nil {
			slog.Debug("storage upload request",
				"method", http.MethodPut,
				"host", parsed.Host,
				"path", parsed.Path,
				"has_query", parsed.RawQuery != "",
			)
		} else {
			slog.Debug("storage upload request", "method", http.MethodPut, "url_redacted", true, "error", parseErr)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, f)
	if err != nil {
		return err
	}

	req.ContentLength = info.Size()
	req.Header.Set("Authorization", token)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		slog.Debug("storage upload response", "status", resp.Status, "status_code", resp.StatusCode, "body_size", len(body))

		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil
}
