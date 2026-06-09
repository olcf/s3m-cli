package servercmd

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/olcf/s3m-cli/internal/auth"
	grpcclient "github.com/olcf/s3m-cli/internal/grpc"
	"github.com/olcf/s3m-cli/internal/runtime"
)

func TestTokenCacheStoreAndLoad(t *testing.T) {
	cache := newTokenCache(nil)

	rec := auth.TokenRecord{
		Token:   "test-token",
		Project: "proj",
		Enclave: "enc",
		Scopes:  []string{"/test/*"},
	}

	cache.storeRecord("test-token", rec)

	got, ok := cache.loadCachedRecord("test-token")
	if !ok {
		t.Fatal("expected cached record to be found")
	}

	if got.Token != rec.Token || got.Project != rec.Project {
		t.Fatalf("cached record mismatch: got %+v, want %+v", got, rec)
	}
}

func TestTokenCacheExpiration(t *testing.T) {
	cache := newTokenCache(nil)

	rec := auth.TokenRecord{Token: "expired-token", Project: "proj", Enclave: "enc"}

	// Store with already-expired time
	cache.mu.Lock()
	cache.cache["expired-token"] = &tokenCacheEntry{
		record:    rec,
		expiresAt: time.Now().Add(-1 * time.Hour),
		element:   cache.order.PushFront("expired-token"),
	}
	cache.mu.Unlock()

	_, ok := cache.loadCachedRecord("expired-token")
	if ok {
		t.Fatal("expected expired record to not be found")
	}

	cache.mu.RLock()
	_, stillPresent := cache.cache["expired-token"]
	cache.mu.RUnlock()
	if stillPresent {
		t.Fatal("expected expired record to be removed from cache")
	}
}

func TestTokenCacheMiss(t *testing.T) {
	cache := newTokenCache(nil)

	_, ok := cache.loadCachedRecord("nonexistent")
	if ok {
		t.Fatal("expected cache miss for nonexistent token")
	}
}

func TestTokenCacheEmptyToken(t *testing.T) {
	cache := newTokenCache(nil)

	_, ok := cache.GetRecord(context.Background(), "")
	if ok {
		t.Fatal("expected empty token to return false")
	}
}

func TestTokenCacheLookupStoredRecord(t *testing.T) {
	state := auth.NewState()
	rec := auth.TokenRecord{
		Token:   "stored-token",
		Project: "proj",
		Enclave: "enc",
		Scopes:  []string{"/test/*"},
	}

	if err := state.PutToken(rec); err != nil {
		t.Fatalf("PutToken: %v", err)
	}

	rt := &runtime.Runtime{State: state}
	cache := newTokenCache(rt)

	got, ok := cache.lookupStoredRecord("stored-token")
	if !ok {
		t.Fatal("expected stored record to be found")
	}

	if got.Token != rec.Token {
		t.Fatalf("stored record mismatch: got %+v, want %+v", got, rec)
	}
}

func TestTokenCacheLookupStoredRecordNilState(t *testing.T) {
	cache := newTokenCache(nil)

	_, ok := cache.lookupStoredRecord("any-token")
	if ok {
		t.Fatal("expected lookup to fail with nil runtime")
	}

	cache2 := newTokenCache(&runtime.Runtime{})

	_, ok = cache2.lookupStoredRecord("any-token")
	if ok {
		t.Fatal("expected lookup to fail with nil state")
	}
}

func TestGetPermissionsWithScopes(t *testing.T) {
	cache := newTokenCache(nil)

	rec := auth.TokenRecord{
		Token:   "scoped-token",
		Project: "proj",
		Enclave: "enc",
		Scopes:  []string{"/test.Service/*"},
	}
	cache.storeRecord("scoped-token", rec)

	perms := cache.GetPermissions(context.Background(), "scoped-token")

	if !perms.Known {
		t.Fatal("expected permissions to be known")
	}

	if len(perms.Raw) != 1 || perms.Raw[0] != "/test.Service/*" {
		t.Fatalf("unexpected scopes: %v", perms.Raw)
	}
}

func TestGetPermissionsUnknownToken(t *testing.T) {
	cache := newTokenCache(nil)

	perms := cache.GetPermissions(context.Background(), "unknown-token")

	if perms.Known {
		t.Fatal("expected permissions to be unknown for missing token")
	}
}

func TestGetPermissionsFallsBackToIntrospection(t *testing.T) {
	rt := &runtime.Runtime{Target: "unused-target"}
	cache := newTokenCache(rt)

	introspected := auth.TokenRecord{
		Token:   "unknown-token",
		Project: "proj",
		Enclave: "enc",
		Scopes:  []string{"/test.Service/*"},
	}

	var calls int
	cache.introspect = func(ctx context.Context, gotRT *runtime.Runtime, token string) (auth.TokenRecord, error) {
		calls++

		if ctx == nil {
			t.Fatal("expected context to be passed to introspection")
		}
		if gotRT != rt {
			t.Fatal("expected introspection to receive the cache runtime")
		}
		if token != "unknown-token" {
			t.Fatalf("expected token unknown-token, got %q", token)
		}

		return introspected, nil
	}

	perms := cache.GetPermissions(context.Background(), "unknown-token")

	if !perms.Known {
		t.Fatal("expected introspected permissions to be known")
	}
	if len(perms.Raw) != 1 || perms.Raw[0] != "/test.Service/*" {
		t.Fatalf("unexpected scopes after introspection: %v", perms.Raw)
	}
	if calls != 1 {
		t.Fatalf("expected one introspection call, got %d", calls)
	}

	perms = cache.GetPermissions(context.Background(), "unknown-token")

	if !perms.Known {
		t.Fatal("expected cached introspected permissions to stay known")
	}
	if calls != 1 {
		t.Fatalf("expected cached lookup to avoid re-introspection, got %d calls", calls)
	}
}

func TestGetPermissionsReturnsUnknownWhenIntrospectionFails(t *testing.T) {
	rt := &runtime.Runtime{Target: "unused-target"}
	cache := newTokenCache(rt)

	cache.introspect = func(context.Context, *runtime.Runtime, string) (auth.TokenRecord, error) {
		return auth.TokenRecord{}, errors.New("boom")
	}

	perms := cache.GetPermissions(context.Background(), "unknown-token")

	if perms.Known {
		t.Fatal("expected failed introspection to leave permissions unknown")
	}
}

func TestVarsFromToken(t *testing.T) {
	cache := newTokenCache(nil)

	rec := auth.TokenRecord{
		Token:     "var-token",
		Project:   "myproject",
		Enclave:   "myenclave",
		Username:  "testuser",
		OwnerName: "testowner",
	}
	cache.storeRecord("var-token", rec)

	ctx := grpcclient.ContextWithAuthToken(context.Background(), "var-token")
	vars := cache.Vars(ctx)

	if vars == nil {
		t.Fatal("expected vars to be non-nil")
	}

	expected := map[string]string{
		"auth.project":   "myproject",
		"project":        "myproject",
		"auth.enclave":   "myenclave",
		"enclave":        "myenclave",
		"auth.username":  "testuser",
		"auth.ownerName": "testowner",
	}

	for k, want := range expected {
		if got := vars[k]; got != want {
			t.Errorf("vars[%q] = %q, want %q", k, got, want)
		}
	}
}

func TestVarsNoToken(t *testing.T) {
	cache := newTokenCache(nil)

	vars := cache.Vars(context.Background())
	if vars != nil {
		t.Fatalf("expected nil vars without token, got %v", vars)
	}
}

func TestVarsFallbackToCurrentToken(t *testing.T) {
	state := auth.NewState()
	rec := auth.TokenRecord{
		Token:   "current-token",
		Project: "currentproj",
		Enclave: "currentenc",
	}

	if err := state.PutToken(rec); err != nil {
		t.Fatalf("PutToken: %v", err)
	}

	rt := &runtime.Runtime{State: state}
	cache := newTokenCache(rt)

	// Context without token - should fall back to current token from state
	vars := cache.Vars(context.Background())

	if vars == nil {
		t.Fatal("expected vars from fallback to current token")
	}

	if vars["project"] != "currentproj" {
		t.Errorf("expected project=currentproj, got %q", vars["project"])
	}
}

func TestTokenCacheConcurrentAccess(t *testing.T) {
	cache := newTokenCache(nil)

	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			rec := auth.TokenRecord{
				Token:   "token",
				Project: "proj",
				Enclave: "enc",
			}
			cache.storeRecord("token", rec)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()
			cache.loadCachedRecord("token")
		}()
	}

	wg.Wait()
}

func TestTokenCacheEvictsLeastRecentlyUsed(t *testing.T) {
	cache := newTokenCache(nil)

	for i := 0; i < tokenCacheMaxEntries; i++ {
		token := fmt.Sprintf("token-%d", i)
		cache.storeRecord(token, auth.TokenRecord{
			Token:   token,
			Project: "proj",
			Enclave: "enc",
		})
	}

	if _, ok := cache.loadCachedRecord("token-0"); !ok {
		t.Fatal("expected oldest token to be present before refresh")
	}

	cache.storeRecord("token-overflow", auth.TokenRecord{
		Token:   "token-overflow",
		Project: "proj",
		Enclave: "enc",
	})

	if _, ok := cache.loadCachedRecord("token-1"); ok {
		t.Fatal("expected least recently used token to be evicted")
	}

	if _, ok := cache.loadCachedRecord("token-0"); !ok {
		t.Fatal("expected recently used token to stay cached")
	}

	cache.mu.RLock()
	size := len(cache.cache)
	cache.mu.RUnlock()
	if size != tokenCacheMaxEntries {
		t.Fatalf("expected cache size %d, got %d", tokenCacheMaxEntries, size)
	}
}
