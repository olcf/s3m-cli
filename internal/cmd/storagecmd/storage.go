package storagecmd

import (
	"github.com/urfave/cli/v3"

	"github.com/olcf/s3m-cli/internal/runtime"
)

func BuildStorageCommand(rt *runtime.Runtime) *cli.Command {
	return &cli.Command{
		Name:  "storage",
		Usage: "Upload and download files from S3M Storage",
		Commands: []*cli.Command{
			buildPushCommand(rt),
			buildPullCommand(rt),
			buildLsCommand(rt),
			buildDeleteCommand(rt),
		},
	}
}
