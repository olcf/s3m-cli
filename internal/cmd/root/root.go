package root

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"

	buildinfo "github.com/olcf/s3m-cli"
	"github.com/olcf/s3m-cli/internal/cmd"
	"github.com/olcf/s3m-cli/internal/cmd/authcmd"
	"github.com/olcf/s3m-cli/internal/cmd/execcmd"
	"github.com/olcf/s3m-cli/internal/cmd/servercmd"
	"github.com/olcf/s3m-cli/internal/cmd/storagecmd"
	"github.com/olcf/s3m-cli/internal/completion"
	"github.com/olcf/s3m-cli/internal/runtime"
)

//nolint:funlen
func Build(rt *runtime.Runtime, appName string) *cli.Command {
	execCmd := execcmd.BuildExecCommand(rt)
	loginCmd := authcmd.BuildLoginCommand(rt)
	authCmd := authcmd.BuildAuthCommand(rt)
	storageCmd := storagecmd.BuildStorageCommand(rt)
	mcpCmd := servercmd.BuildMCPCommand(rt)
	openapiCmd := servercmd.BuildOpenAPICommand(rt)

	if strings.TrimSpace(appName) == "" {
		appName = "s3m"
	}

	appName = filepath.Base(appName)

	return &cli.Command{
		Name:                  appName,
		Version:               buildinfo.Version,
		Usage:                 "Interact with S3M APIs and MCP/OpenAPI servers",
		EnableShellCompletion: true,
		ConfigureShellCompletionCommand: func(cmd *cli.Command) {
			cmd.Hidden = false

			orig := cmd.Action
			cmd.Action = func(ctx context.Context, c *cli.Command) error {
				if c.Args().Len() > 0 && strings.EqualFold(c.Args().First(), "fish") {
					_, err := c.Writer.Write([]byte(completion.GenerateFish(appName)))

					return err
				}

				return orig(ctx, c)
			}
		},
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "debug", Usage: "Enable debug logging"},
			&cli.StringFlag{Name: "grpc-target", Value: cmd.DefaultS3MEndpoint, Usage: "gRPC target host:port"},
			&cli.BoolFlag{Name: "ignore-permissions",
				Usage: "Ignore introspected token scopes when listing tools or checking exec availability"},
		},
		Before: func(ctx context.Context, c *cli.Command) (context.Context, error) {
			rt.Debug = c.Bool("debug")
			rt.Target = c.String("grpc-target")
			rt.IgnorePermissions = c.Bool("ignore-permissions")
			handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: func() slog.Level {
					if rt.Debug {
						return slog.LevelDebug
					}

					return slog.LevelError
				}(),
			})
			slog.SetDefault(slog.New(handler))

			return ctx, nil
		},
		Commands: []*cli.Command{
			loginCmd,
			authCmd,
			execCmd,
			storageCmd,
			mcpCmd,
			openapiCmd,
		},
	}
}
