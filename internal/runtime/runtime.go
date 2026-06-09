package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/olcf/s3m-cli/internal/auth"
	"github.com/olcf/s3m-cli/internal/cmd"
	"github.com/olcf/s3m-cli/internal/docs"
	"github.com/olcf/s3m-cli/internal/embedded"
	grpcclient "github.com/olcf/s3m-cli/internal/grpc"
	"github.com/olcf/s3m-cli/internal/headermap"
	"github.com/olcf/s3m-cli/internal/permissions"
	"github.com/olcf/s3m-cli/internal/proto"
)

type Runtime struct {
	State             *auth.State
	StatePath         string
	Files             *protoregistry.Files
	Methods           []proto.MethodInfo
	docs              *docs.Store
	docsMu            sync.RWMutex
	Target            string
	Debug             bool
	IgnorePermissions bool
	StorageTokens     StorageTokens
	stateErr          error
}

func Bootstrap() (*Runtime, error) {
	rt := &Runtime{
		Target: cmd.DefaultS3MEndpoint,
		StorageTokens: StorageTokens{
			PullToken: os.Getenv("S3MIO_PULL_TOKEN"),
			PushToken: os.Getenv("S3MIO_PUSH_TOKEN"),
		},
	}

	rt.loadState()

	files, err := LoadDescriptorFiles(embedded.DescriptorSet)
	if err != nil {
		return nil, err
	}

	enclave := headermap.DefaultEnclave
	if rec, ok := rt.State.CurrentToken(); ok && strings.TrimSpace(rec.Enclave) != "" {
		enclave = rec.Enclave
	}

	methods := proto.CollectMethods(files, enclave, headermap.DefaultResolver())

	rt.Files = files
	rt.Methods = methods

	return rt, nil
}

func (rt *Runtime) EnsureState() error {
	if rt == nil {
		return errors.New("load auth state: runtime is nil")
	}

	if rt.State != nil {
		return nil
	}

	rt.loadState()

	if rt.State != nil {
		return nil
	}

	return formatStateError(rt.StatePath, rt.stateErr)
}

func (rt *Runtime) PermissionSnapshot() permissions.Snapshot {
	if rt == nil {
		return permissions.New(nil, false)
	}

	if rt.IgnorePermissions {
		return permissions.New([]string{"*"}, true)
	}

	if err := rt.EnsureState(); err != nil {
		return permissions.New(nil, false)
	}

	rec, ok := rt.State.CurrentToken()
	if !ok {
		return permissions.New(nil, false)
	}

	return permissions.New(rec.Scopes, rec.IntrospectionOK())
}

func (rt *Runtime) CurrentToken() (auth.TokenRecord, bool) {
	if err := rt.EnsureState(); err != nil {
		return auth.TokenRecord{}, false
	}

	return rt.State.CurrentToken()
}

func (rt *Runtime) StoredTokens() []auth.TokenRecord {
	if err := rt.EnsureState(); err != nil {
		return nil
	}

	records := make([]auth.TokenRecord, 0, len(rt.State.Tokens))
	for _, rec := range rt.State.Tokens {
		records = append(records, rec)
	}

	return records
}

func (rt *Runtime) GetDocs() *docs.Store {
	rt.docsMu.RLock()
	defer rt.docsMu.RUnlock()

	return rt.docs
}

func (rt *Runtime) SetDocs(store *docs.Store) {
	rt.docsMu.Lock()
	defer rt.docsMu.Unlock()

	rt.docs = store
}

func (rt *Runtime) Dial(ctx context.Context, token string) (*grpc.ClientConn, error) {
	return grpcclient.DialAndWait(ctx, rt.Target, token, cmd.GRPCConnectTimeout, rt.Debug)
}

//
// Storage tokens

// StorageTokens holds S3MIO storage tokens injected into the environment by
// compute-utils for Slurm jobs. Used as fallback auth for storage operations
// when the primary auth state token is unavailable or expired.
type StorageTokens struct {
	PullToken string // from S3MIO_PULL_TOKEN — read operations
	PushToken string // from S3MIO_PUSH_TOKEN — write operations
}

type StorageOp int

const (
	StorageOpRead StorageOp = iota
	StorageOpWrite
	StorageOpDelete
)

// StorageToken returns the S3MIO environment token for the given operation type.
// Read operations use S3MIO_PULL_TOKEN, write operations use S3MIO_PUSH_TOKEN.
// Delete operations have no S3MIO equivalent.
func (rt *Runtime) StorageToken(op StorageOp) (string, bool) {
	switch op {
	// ListDatasets, GetDatasetContents, GetDownloadURLs
	case StorageOpRead:
		if rt.StorageTokens.PullToken != "" {
			return rt.StorageTokens.PullToken, true
		}
	// ReserveDataset, CommitDataset
	case StorageOpWrite:
		if rt.StorageTokens.PushToken != "" {
			return rt.StorageTokens.PushToken, true
		}
	// DeleteDataset
	case StorageOpDelete:
		// No S3MIO token for delete.
	}

	return "", false
}

func (rt *Runtime) loadState() {
	if rt == nil || rt.State != nil || rt.stateErr != nil {
		return
	}

	statePath := strings.TrimSpace(rt.StatePath)
	if statePath == "" {
		var err error

		statePath, err = auth.DefaultStatePath()
		if err != nil {
			rt.stateErr = err
			return
		}

		rt.StatePath = statePath
	}

	state, err := auth.LoadState(statePath)
	if err != nil {
		rt.stateErr = err
		return
	}

	rt.State = state
}

func formatStateError(path string, err error) error {
	if err == nil {
		return errors.New("load auth state: state is unavailable")
	}

	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("load auth state: %w", err)
	}

	return fmt.Errorf("load auth state %q: %w", path, err)
}
