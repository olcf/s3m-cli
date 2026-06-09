package servercmd

import (
	"container/list"
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/olcf/s3m-cli/internal/auth"
	"github.com/olcf/s3m-cli/internal/cmd"
	grpcclient "github.com/olcf/s3m-cli/internal/grpc"
	"github.com/olcf/s3m-cli/internal/permissions"
	"github.com/olcf/s3m-cli/internal/runtime"
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
}

type tokenCacheEntry struct {
	record    auth.TokenRecord
	expiresAt time.Time
	element   *list.Element
}

const tokenCacheTTL = 2 * time.Hour
const tokenCacheMaxEntries = 2048

func newTokenCache(rt *runtime.Runtime) *tokenCache {
	return &tokenCache{
		rt:         rt,
		introspect: introspectTokenRecord,
		cache:      make(map[string]*tokenCacheEntry),
		order:      list.New(),
	}
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
	rec, ok := c.GetRecord(ctx, token)
	if !ok {
		return permissions.New(nil, false)
	}

	return permissions.New(rec.Scopes, rec.IntrospectionOK())
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
	if token == "" {
		return auth.TokenRecord{}, false
	}

	if cached, ok := c.loadCachedRecord(token); ok {
		return cached, true
	}

	if rec, ok := c.lookupStoredRecord(token); ok {
		c.storeRecord(token, rec)
		return rec, true
	}

	rec, err := c.introspectToken(ctx, token)
	if err != nil {
		slog.Debug("token introspection failed", "error", err)
		return auth.TokenRecord{}, false
	}

	c.storeRecord(token, rec)

	return rec, true
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
