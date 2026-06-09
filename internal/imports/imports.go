// Package imports ensures all s3m-apis packages are included in the vendor
// directory by providing blank imports for packages not otherwise referenced
// by the main module's source code.
package imports

import (
	_ "github.com/olcf/s3m-apis/pkg/s3mutil"
	_ "github.com/olcf/s3m-apis/slurm/v0043"
	_ "github.com/olcf/s3m-apis/status/v1alpha"
	_ "github.com/olcf/s3m-apis/streaming/v1alpha"
)
