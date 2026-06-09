package storagecmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"syscall"

	"github.com/urfave/cli/v3"

	"github.com/olcf/s3m-cli/internal/output"
	"github.com/olcf/s3m-cli/internal/runtime"

	storagepb "github.com/olcf/s3m-apis/storage/v1alpha"
)

func buildPullCommand(rt *runtime.Runtime) *cli.Command {
	return &cli.Command{
		Name:      "pull",
		Usage:     "Download files from a dataset",
		ArgsUsage: "<dataset> [<pattern>] <local-path>",
		Flags: []cli.Flag{
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
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			return runPull(ctx, c, rt)
		},
	}
}

func runPull(ctx context.Context, c *cli.Command, rt *runtime.Runtime) error {
	if c.NArg() < 2 || c.NArg() > 3 {
		return errors.New("usage: s3m storage pull <dataset> [<pattern>] <local-path>")
	}

	var dataset, pattern, localPath string

	if c.NArg() == 2 {
		dataset = c.Args().Get(0)
		pattern = "**"
		localPath = c.Args().Get(1)
	} else {
		dataset = c.Args().Get(0)
		pattern = c.Args().Get(1)
		localPath = c.Args().Get(2)
	}

	return withClient(ctx, c, rt, runtime.StorageOpRead, func(client storagepb.BucketGatewayClient, token string) error {
		selector, err := buildDatasetSelector(dataset, c.Bool("id"), c.Bool("latest"))
		if err != nil {
			return err
		}

		selector, err = resolveDatasetSelectorID(ctx, client, selector)
		if err != nil {
			return err
		}

		return pullDataset(ctx, client, selector, pattern, localPath, token, c)
	})
}

//nolint:funlen
func pullDataset(
	ctx context.Context, client storagepb.BucketGatewayClient,
	selector *storagepb.DatasetSelector, pattern, localPath, token string,
	c *cli.Command,
) error {
	paths, err := resolvePaths(ctx, client, selector, pattern)
	if err != nil {
		return err
	}

	format := output.Format(c.String("output"))
	out := output.NewOutput(format, os.Stdout)

	if len(paths) == 0 {
		out.Infof("No files to download")
		return out.Render()
	}

	resp, err := client.GetDownloadURLs(ctx, &storagepb.GetDownloadURLsRequest{
		Selector: selector,
		PathFilter: &storagepb.GetDownloadURLsRequest_Paths{
			Paths: &storagepb.PathList{Paths: paths},
		},
	})
	if err != nil {
		return fmt.Errorf("get download URLs: %w", err)
	}

	httpClient := newTransferHTTPClient(transferResponseHeaderTimeout)
	multiFile := isGlobPattern(pattern)

	absBase, err := filepath.Abs(localPath)
	if err != nil {
		return fmt.Errorf("resolve local path: %w", err)
	}

	resolvedBase, err := resolvePathForCreate(absBase)
	if err != nil {
		return fmt.Errorf("resolve download directory: %w", err)
	}

	var totalBytes int64

	for _, target := range resp.GetDownloads() {
		p := target.GetPath()

		outPath := localPath
		if multiFile {
			outPath = filepath.Join(localPath, p)

			absOut, err := filepath.Abs(outPath)
			if err != nil {
				return fmt.Errorf("resolve output path: %w", err)
			}

			if !pathWithinBase(absBase, absOut) {
				return fmt.Errorf("refusing to write outside download directory: %s", p)
			}

			resolvedOutDir, err := resolvePathForCreate(filepath.Dir(absOut))
			if err != nil {
				return fmt.Errorf("resolve output directory: %w", err)
			}

			if !pathWithinBase(resolvedBase, resolvedOutDir) {
				return fmt.Errorf("refusing to write outside download directory: %s", p)
			}
		}

		out.Infof("Downloading %s...", p)

		if err := downloadFile(ctx, httpClient, target.GetDownloadUrl(), outPath, token); err != nil {
			return fmt.Errorf("download %s: %w", p, err)
		}

		if stat, err := os.Stat(outPath); err == nil {
			totalBytes += stat.Size()
		}
	}

	out.Success(fmt.Sprintf("Downloaded %d file(s)", len(resp.GetDownloads())))
	out.AddField("filesDownloaded", len(resp.GetDownloads()))
	out.AddField("totalBytes", totalBytes)
	out.AddField("localPath", localPath)

	return out.Render()
}

//
// Path resolution

func resolvePaths(
	ctx context.Context,
	client storagepb.BucketGatewayClient,
	selector *storagepb.DatasetSelector,
	pattern string,
) ([]string, error) {
	if !isGlobPattern(pattern) {
		return []string{pattern}, nil
	}

	files, err := listAllFiles(ctx, client, selector, pattern)
	if err != nil {
		return nil, fmt.Errorf("list contents: %w", err)
	}

	if len(files) == 0 {
		return nil, nil
	}

	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.GetPath()
	}

	return paths, nil
}

func resolvePathForCreate(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	cleanPath := filepath.Clean(absPath)
	volume := filepath.VolumeName(cleanPath)
	rest := strings.TrimPrefix(cleanPath, volume)
	current := volume

	if strings.HasPrefix(rest, string(os.PathSeparator)) {
		current += string(os.PathSeparator)
		rest = strings.TrimPrefix(rest, string(os.PathSeparator))
	}

	if rest == "" {
		if current == "" {
			return string(os.PathSeparator), nil
		}

		return current, nil
	}

	for part := range strings.SplitSeq(rest, string(os.PathSeparator)) {
		if part == "" || part == "." {
			continue
		}

		current = filepath.Join(current, part)

		info, err := os.Lstat(current)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}

			return "", err
		}

		if info.Mode()&os.ModeSymlink == 0 {
			continue
		}

		resolved, err := filepath.EvalSymlinks(current)
		if err != nil {
			return "", err
		}

		current = resolved
	}

	return current, nil
}

func pathWithinBase(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}

	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

//
// File operations

func downloadFile(ctx context.Context, client *http.Client, url, outPath, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", token)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return writeFile(outPath, resp.Body)
}

func writeFile(outPath string, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(outPath), 0o750); err != nil {
		return err
	}

	mode := os.FileMode(defaultOutputMode)
	if info, err := os.Stat(outPath); err == nil {
		mode = info.Mode().Perm()
	}

	tmp, err := os.CreateTemp(filepath.Dir(outPath), "."+filepath.Base(outPath)+".tmp-*")
	if err != nil {
		return err
	}

	tmpPath := tmp.Name()
	committed := false

	defer func() {
		if committed {
			return
		}

		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()

	if err := tmp.Chmod(mode); err != nil {
		return err
	}

	if _, err := io.Copy(tmp, r); err != nil {
		return err
	}

	if err := tmp.Sync(); err != nil {
		return err
	}

	if err := tmp.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, outPath); err != nil {
		return err
	}

	committed = true

	return syncDir(filepath.Dir(outPath))
}

const defaultOutputMode = 0o644

func syncDir(dir string) error {
	// #nosec G304 -- dir is derived from validated output paths.
	// Single-file mode intentionally honors the CLI destination.
	f, err := os.Open(dir)
	if err != nil {
		return err
	}

	defer func() { _ = f.Close() }()

	if err := f.Sync(); err != nil && !ignorableDirSyncError(err) {
		return err
	}

	return nil
}

func ignorableDirSyncError(err error) bool {
	if goruntime.GOOS == "windows" {
		return true
	}

	return errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP)
}
