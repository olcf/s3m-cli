package servercmd

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sync"
	"time"

	"github.com/olcf/s3m-cli/internal/auth"
	"github.com/olcf/s3m-cli/internal/cmd"
	grpcclient "github.com/olcf/s3m-cli/internal/grpc"
	"github.com/olcf/s3m-cli/internal/permissions"
	"github.com/olcf/s3m-cli/internal/runtime"
	"google.golang.org/grpc"
)

// tokenCache caches introspected token records with TTL-based expiration.
// It is used in stateless mode to avoid repeated introspection calls for the
// same token.
type tokenCache struct {
	rt         *runtime.Runtime
	introspect func(context.Context, *runtime.Runtime, string) (auth.TokenRecord, error)
	mu         sync.RWMutex
	cache      map[string]*tokenCacheEntry
	order      *list.List
	inflight   map[string]*tokenLookup
	tokenRegex *regexp.Regexp
	minLength  int
	maxLength  int
}

type tokenCacheEntry struct {
	record    auth.TokenRecord
	expiresAt time.Time
	element   *list.Element
}

type tokenLookup struct {
	done   chan struct{}
	record auth.TokenRecord
	ok     bool
	err    error
}

const tokenCacheTTL = 2 * time.Hour
const tokenCacheMaxEntries = 2048

const (
	defaultS3MJWTRegex       = `^eyJh[A-Za-z0-9_-]*\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$`
	defaultS3MTokenMinLength = 300
	defaultS3MTokenMaxLength = 4096
)

func newTokenCache(rt *runtime.Runtime, connections ...grpc.ClientConnInterface) *tokenCache {
	cache, err := newTokenCacheWithValidation(
		rt,
		defaultS3MJWTRegex,
		defaultS3MTokenMinLength,
		defaultS3MTokenMaxLength,
		connections...,
	)
	if err != nil {
		panic(err)
	}

	return cache
}

func newTokenCacheWithValidation(
	rt *runtime.Runtime,
	tokenRegex string,
	minLength int,
	maxLength int,
	connections ...grpc.ClientConnInterface,
) (*tokenCache, error) {
	if minLength < 0 {
		return nil, errors.New("S3M token minimum length must not be negative")
	}

	if maxLength < minLength {
		return nil, errors.New("S3M token maximum length must be at least the minimum length")
	}

	compiledTokenRegex, err := regexp.Compile(tokenRegex)
	if err != nil {
		return nil, fmt.Errorf("compile S3M token regex: %w", err)
	}

	cache := &tokenCache{
		rt:         rt,
		introspect: introspectTokenRecord,
		cache:      make(map[string]*tokenCacheEntry),
		order:      list.New(),
		inflight:   make(map[string]*tokenLookup),
		tokenRegex: compiledTokenRegex,
		minLength:  minLength,
		maxLength:  maxLength,
	}

	if len(connections) > 0 && connections[0] != nil {
		conn := connections[0]
		cache.introspect = func(
			ctx context.Context, _ *runtime.Runtime, token string,
		) (auth.TokenRecord, error) {
			res := auth.IntrospectTokenWithConn(ctx, conn, token, cmd.GRPCCallTimeout)
			if res.Err != nil {
				return auth.TokenRecord{}, res.Err
			}

			return res.Record, nil
		}
	}

	return cache, nil
}

func introspectTokenRecord(
	ctx context.Context,
	rt *runtime.Runtime,
	token string,
) (auth.TokenRecord, error) {
	res := auth.IntrospectToken(ctx, rt.Target, token, cmd.GRPCConnectTimeout, cmd.GRPCCallTimeout, rt.Debug)
	if res.Err != nil {
		return auth.TokenRecord{}, res.Err
	}

	return res.Record, nil
}

// Vars returns doc variable mappings from the token in context.
// This method is compatible with toolset.BuildDocToolSet's resolver signature.
func (c *tokenCache) Vars(ctx context.Context) map[string]string {
	rec, ok := c.resolveRecord(ctx)
	if !ok {
		return nil
	}

	vars := make(map[string]string)

	if rec.Project != "" {
		vars["auth.project"] = rec.Project
		vars["project"] = rec.Project
	}

	if rec.Enclave != "" {
		vars["auth.enclave"] = rec.Enclave
		vars["enclave"] = rec.Enclave
	}

	if rec.Username != "" {
		vars["auth.username"] = rec.Username
	}

	if rec.OwnerName != "" {
		vars["auth.ownerName"] = rec.OwnerName
	}

	return vars
}

// GetRecord returns the cached or introspected token record for the given
// token.
func (c *tokenCache) GetRecord(ctx context.Context, token string) (auth.TokenRecord, bool) {
	return c.recordForToken(ctx, token)
}

// GetPermissions returns a permissions snapshot for the given token.
func (c *tokenCache) GetPermissions(ctx context.Context, token string) permissions.Snapshot {
	perms, _ := c.getPermissions(ctx, token)

	return perms
}

func (c *tokenCache) getPermissions(ctx context.Context, token string) (permissions.Snapshot, error) {
	rec, ok, err := c.recordForTokenResult(ctx, token)
	if !ok {
		return permissions.New(nil, false), err
	}

	return permissions.New(rec.Scopes, rec.IntrospectionOK()), nil
}

func (c *tokenCache) resolveRecord(ctx context.Context) (auth.TokenRecord, bool) {
	if token, ok := grpcclient.AuthTokenFromContext(ctx); ok {
		rec, ok := c.recordForToken(ctx, token)
		return rec, ok
	}

	if c.rt == nil {
		return auth.TokenRecord{}, false
	}

	return c.rt.CurrentToken()
}

func (c *tokenCache) recordForToken(ctx context.Context, token string) (auth.TokenRecord, bool) {
	rec, ok, _ := c.recordForTokenResult(ctx, token)

	return rec, ok
}

func (c *tokenCache) recordForTokenResult(ctx context.Context, token string) (auth.TokenRecord, bool, error) {
	if token == "" {
		return auth.TokenRecord{}, false, nil
	}

	if cached, ok := c.loadCachedRecord(token); ok {
		return cached, true, nil
	}

	c.mu.Lock()
	if pending, ok := c.inflight[token]; ok {
		c.mu.Unlock()

		select {
		case <-pending.done:
			return pending.record, pending.ok, pending.err
		case <-ctx.Done():
			return auth.TokenRecord{}, false, ctx.Err()
		}
	}

	pending := &tokenLookup{done: make(chan struct{})}
	c.inflight[token] = pending
	c.mu.Unlock()

	rec, ok, err := c.lookupToken(ctx, token)

	c.mu.Lock()
	pending.record = rec
	pending.ok = ok
	pending.err = err

	delete(c.inflight, token)
	close(pending.done)
	c.mu.Unlock()

	return rec, ok, err
}

func (c *tokenCache) lookupToken(ctx context.Context, token string) (auth.TokenRecord, bool, error) {
	if cached, ok := c.loadCachedRecord(token); ok {
		return cached, true, nil
	}

	if rec, ok := c.lookupStoredRecord(token); ok {
		c.storeRecord(token, rec)
		return rec, true, nil
	}

	if !c.looksLikeS3MToken(token) {
		return auth.TokenRecord{}, false, nil
	}

	rec, err := c.introspectToken(ctx, token)
	if err != nil {
		slog.Debug("token introspection failed", "error", err)
		return auth.TokenRecord{}, false, err
	}

	c.storeRecord(token, rec)

	return rec, true, nil
}

func (c *tokenCache) looksLikeS3MToken(token string) bool {
	return len(token) >= c.minLength &&
		len(token) <= c.maxLength &&
		c.tokenRegex.MatchString(token)
}

func (c *tokenCache) loadCachedRecord(token string) (auth.TokenRecord, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.loadCachedRecordLocked(token)
}

func (c *tokenCache) loadCachedRecordLocked(token string) (auth.TokenRecord, bool) {
	entry, ok := c.cache[token]
	if !ok {
		return auth.TokenRecord{}, false
	}

	if time.Now().After(entry.expiresAt) {
		c.removeEntryLocked(token, entry)

		return auth.TokenRecord{}, false
	}

	c.order.MoveToFront(entry.element)

	return entry.record, true
}

func (c *tokenCache) storeRecord(token string, rec auth.TokenRecord) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, ok := c.cache[token]; ok {
		entry.record = rec
		entry.expiresAt = time.Now().Add(tokenCacheTTL)
		c.order.MoveToFront(entry.element)
	} else {
		c.cache[token] = &tokenCacheEntry{
			record:    rec,
			expiresAt: time.Now().Add(tokenCacheTTL),
			element:   c.order.PushFront(token),
		}
	}

	c.evictOverflowLocked()
}

func (c *tokenCache) lookupStoredRecord(token string) (auth.TokenRecord, bool) {
	if c.rt == nil {
		return auth.TokenRecord{}, false
	}

	for _, rec := range c.rt.StoredTokens() {
		if rec.Token == token {
			return rec, true
		}
	}

	return auth.TokenRecord{}, false
}

func (c *tokenCache) introspectToken(ctx context.Context, token string) (auth.TokenRecord, error) {
	if c.rt == nil {
		return auth.TokenRecord{}, errors.New("runtime missing")
	}

	if ctx == nil {
		return auth.TokenRecord{}, errors.New("context missing")
	}

	if c.introspect == nil {
		return auth.TokenRecord{}, errors.New("introspection function missing")
	}

	return c.introspect(ctx, c.rt, token)
}

func (c *tokenCache) evictOverflowLocked() {
	for len(c.cache) > tokenCacheMaxEntries {
		back := c.order.Back()
		if back == nil {
			return
		}

		token, ok := back.Value.(string)
		if !ok {
			c.order.Remove(back)
			continue
		}

		entry, found := c.cache[token]
		if !found {
			c.order.Remove(back)
			continue
		}

		c.removeEntryLocked(token, entry)
	}
}

func (c *tokenCache) removeEntryLocked(token string, entry *tokenCacheEntry) {
	if entry != nil && entry.element != nil {
		c.order.Remove(entry.element)
	}

	delete(c.cache, token)
}
