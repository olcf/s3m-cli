package servercmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	slurmv0042pb "github.com/olcf/s3m-apis/slurm/v0042"
	storagepb "github.com/olcf/s3m-apis/storage/v1alpha"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/olcf/s3m-cli/internal/auth"
	grpcclient "github.com/olcf/s3m-cli/internal/grpc"
	"github.com/olcf/s3m-cli/internal/permissions"
	"github.com/olcf/s3m-cli/internal/proto"
	"github.com/olcf/s3m-cli/internal/runtime"
	"github.com/olcf/s3m-cli/internal/toolset"
)

func TestFilterAllowedUnknownPermissions(t *testing.T) {
	perms := permissions.New(nil, false)

	methods := []proto.MethodInfo{
		{Service: nil, Method: nil},
		{Service: nil, Method: nil},
	}

	allowed := filterAllowed(perms, methods)

	if len(allowed) != len(methods) {
		t.Fatalf("unknown permissions should preserve all methods, got %d", len(allowed))
	}
}

func TestFilterAllowedWithScopes(t *testing.T) {
	file := slurmv0042pb.File_proto_slurm_v0042_slurm_proto
	service := file.Services().ByName("SlurmIndirect")
	getJobs := service.Methods().ByName("GetJobs")
	getNodes := service.Methods().ByName("GetNodes")

	methods := []proto.MethodInfo{
		{Service: service, Method: getJobs},
		{Service: service, Method: getNodes},
	}

	// Only allow GetJobs
	perms := permissions.New([]string{"/olcf.s3m.slurm.v0042.SlurmIndirect/GetJobs"}, true)

	allowed := filterAllowed(perms, methods)

	if len(allowed) != 1 {
		t.Fatalf("expected 1 allowed method, got %d", len(allowed))
	}

	if string(allowed[0].Method.Name()) != "GetJobs" {
		t.Fatalf("expected GetJobs to be allowed, got %s", allowed[0].Method.Name())
	}
}

func TestFilterAllowedWildcard(t *testing.T) {
	file := slurmv0042pb.File_proto_slurm_v0042_slurm_proto
	service := file.Services().ByName("SlurmIndirect")
	getJobs := service.Methods().ByName("GetJobs")
	getNodes := service.Methods().ByName("GetNodes")

	methods := []proto.MethodInfo{
		{Service: service, Method: getJobs},
		{Service: service, Method: getNodes},
	}

	perms := permissions.New([]string{"/olcf.s3m.slurm.v0042.SlurmIndirect/*"}, true)

	allowed := filterAllowed(perms, methods)

	if len(allowed) != 2 {
		t.Fatalf("wildcard should allow all service methods, got %d", len(allowed))
	}
}

func TestFilterAllowedEmptyMethods(t *testing.T) {
	perms := permissions.New([]string{"/test/*"}, true)

	allowed := filterAllowed(perms, nil)

	if allowed != nil {
		t.Fatalf("expected nil for empty methods, got %v", allowed)
	}
}

func TestHasAllowedMethodNames(t *testing.T) {
	file := slurmv0042pb.File_proto_slurm_v0042_slurm_proto
	service := file.Services().ByName("SlurmIndirect")
	method := service.Methods().ByName("GetJobs")

	if hasAllowedMethodNames([]proto.MethodInfo{{Service: service, Method: method}}, "GetDownloadURLs") {
		t.Fatal("unexpected storage method match for non-storage method")
	}
}

func TestWrapWithAuthTokenExtractionSuccess(t *testing.T) {
	var capturedToken string

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := grpcclient.AuthTokenFromContext(r.Context())
		if ok {
			capturedToken = token
		}

		w.WriteHeader(http.StatusOK)
	})

	handler := wrapWithAuthTokenExtraction(inner)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer my-secret-token")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	if capturedToken != "my-secret-token" {
		t.Fatalf("expected token 'my-secret-token', got %q", capturedToken)
	}
}

func TestWrapWithAuthTokenExtractionMissingHeader(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner handler should not be called")
	})

	handler := wrapWithAuthTokenExtraction(inner)

	req := httptest.NewRequest(http.MethodPost, "/", nil)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rr.Code)
	}
}

func TestWrapWithAuthTokenExtractionOptionsPassthrough(t *testing.T) {
	called := false

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})

	handler := wrapWithAuthTokenExtraction(inner)

	req := httptest.NewRequest(http.MethodOptions, "/", nil)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Fatal("OPTIONS request should pass through without auth check")
	}

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rr.Code)
	}
}

func TestWrapStatelessOpenAPIRouteAllowsAuthorizedMethod(t *testing.T) {
	file := slurmv0042pb.File_proto_slurm_v0042_slurm_proto
	service := file.Services().ByName("SlurmIndirect")
	getJobs := service.Methods().ByName("GetJobs")

	cache := newTokenCache(nil)
	cache.storeRecord("jobs-token", auth.TokenRecord{
		Token:   "jobs-token",
		Project: "proj",
		Enclave: "enc",
		Scopes:  []string{"/olcf.s3m.slurm.v0042.SlurmIndirect/GetJobs"},
	})

	called := false
	route := toolset.HTTPRouteSpec{
		Path:               "/jobs",
		RequiresPermission: true,
		Method:             proto.MethodInfo{Service: service, Method: getJobs},
		Handler: func(w http.ResponseWriter, r *http.Request) {
			called = true

			token, ok := grpcclient.AuthTokenFromContext(r.Context())
			if !ok || token != "jobs-token" {
				t.Fatalf("expected token in context, got %q ok=%v", token, ok)
			}

			w.WriteHeader(http.StatusOK)
		},
	}

	handler := wrapStatelessOpenAPIRoute(route, nil, cache)

	req := httptest.NewRequest(http.MethodPost, route.Path, nil)
	req.Header.Set("Authorization", "Bearer jobs-token")

	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if !called {
		t.Fatal("expected wrapped route handler to be called")
	}
}

func TestWrapStatelessOpenAPIRouteRejectsForbiddenMethod(t *testing.T) {
	file := slurmv0042pb.File_proto_slurm_v0042_slurm_proto
	service := file.Services().ByName("SlurmIndirect")
	getNodes := service.Methods().ByName("GetNodes")

	cache := newTokenCache(nil)
	cache.storeRecord("jobs-token", auth.TokenRecord{
		Token:   "jobs-token",
		Project: "proj",
		Enclave: "enc",
		Scopes:  []string{"/olcf.s3m.slurm.v0042.SlurmIndirect/GetJobs"},
	})

	route := toolset.HTTPRouteSpec{
		Path:               "/nodes",
		RequiresPermission: true,
		Method:             proto.MethodInfo{Service: service, Method: getNodes},
		Handler: func(http.ResponseWriter, *http.Request) {
			t.Fatal("forbidden route should not be invoked")
		},
	}

	handler := wrapStatelessOpenAPIRoute(route, nil, cache)

	req := httptest.NewRequest(http.MethodPost, route.Path, nil)
	req.Header.Set("Authorization", "Bearer jobs-token")

	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", rr.Code)
	}
}

func TestWrapStatelessOpenAPIRouteAllowsUnknownPermissions(t *testing.T) {
	file := slurmv0042pb.File_proto_slurm_v0042_slurm_proto
	service := file.Services().ByName("SlurmIndirect")
	getNodes := service.Methods().ByName("GetNodes")

	called := false
	route := toolset.HTTPRouteSpec{
		Path:               "/nodes",
		RequiresPermission: true,
		Method:             proto.MethodInfo{Service: service, Method: getNodes},
		Handler: func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		},
	}

	handler := wrapStatelessOpenAPIRoute(route, nil, newTokenCache(nil))

	req := httptest.NewRequest(http.MethodPost, route.Path, nil)
	req.Header.Set("Authorization", "Bearer unknown-token")

	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if !called {
		t.Fatal("expected route handler to be called when permissions are unknown")
	}
}

func TestWrapStatelessOpenAPIRouteHidesStorageRouteWithoutRequiredMethod(t *testing.T) {
	listDatasets := storageMethodInfo(t, "ListDatasets")
	getDownloadURLs := storageMethodInfo(t, "GetDownloadURLs")

	rt := &runtime.Runtime{
		Methods: []proto.MethodInfo{listDatasets, getDownloadURLs},
	}

	cache := newTokenCache(nil)
	cache.storeRecord("storage-token", auth.TokenRecord{
		Token:   "storage-token",
		Project: "proj",
		Enclave: "enc",
		Scopes:  []string{"/olcf.s3m.storage.v1alpha.BucketGateway/ListDatasets"},
	})

	route := toolset.HTTPRouteSpec{
		Path:                "/storage/read_file",
		RequiresPermission:  true,
		RequiredMethodNames: []string{"GetDownloadURLs"},
		Handler: func(http.ResponseWriter, *http.Request) {
			t.Fatal("forbidden storage route should not be invoked")
		},
	}

	handler := wrapStatelessOpenAPIRoute(route, rt, cache)

	for _, tt := range []struct {
		name   string
		method string
	}{
		{name: "post", method: http.MethodPost},
		{name: "options preflight", method: http.MethodOptions},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, route.Path, nil)
			req.Header.Set("Authorization", "Bearer storage-token")
			if tt.method == http.MethodOptions {
				req.Header.Set("Origin", "https://example.com")
			}

			rr := httptest.NewRecorder()
			handler(rr, req)

			if rr.Code != http.StatusNotFound {
				t.Fatalf("expected status 404, got %d", rr.Code)
			}
		})
	}
}

func TestWrapStatelessOpenAPIRouteAllowsStoragePreflightWithoutToken(t *testing.T) {
	route := toolset.HTTPRouteSpec{
		Path:                "/storage/read_file",
		RequiresPermission:  true,
		RequiredMethodNames: []string{"GetDownloadURLs"},
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodOptions {
				t.Fatalf("expected OPTIONS request, got %s", r.Method)
			}

			w.WriteHeader(http.StatusNoContent)
		},
	}

	handler := wrapStatelessOpenAPIRoute(route, &runtime.Runtime{}, newTokenCache(nil))

	req := httptest.NewRequest(http.MethodOptions, route.Path, nil)
	req.Header.Set("Origin", "https://example.com")

	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rr.Code)
	}
}

func TestWrapStatelessDocRouteFiltersDocsByTokenSurface(t *testing.T) {
	file := slurmv0042pb.File_proto_slurm_v0042_slurm_proto
	service := file.Services().ByName("SlurmIndirect")
	getJobs := service.Methods().ByName("GetJobs")
	getNodes := service.Methods().ByName("GetNodes")

	jobsTool := proto.ToolNameForMethod(file, service, getJobs)
	nodesTool := proto.ToolNameForMethod(file, service, getNodes)

	methods := []proto.MethodInfo{
		{
			File:     file,
			Service:  service,
			Method:   getJobs,
			ToolName: jobsTool,
			Path:     "/jobs",
			Desc:     "Get jobs",
		},
		{
			File:     file,
			Service:  service,
			Method:   getNodes,
			ToolName: nodesTool,
			Path:     "/nodes",
			Desc:     "Get nodes",
		},
	}

	store := loadToolDocStore(t, methods, nodesTool, "Node docs")
	rt := &runtime.Runtime{Methods: methods}
	rt.SetDocs(store)

	cache := newTokenCache(rt)
	cache.storeRecord("jobs-token", auth.TokenRecord{
		Token:   "jobs-token",
		Project: "proj",
		Enclave: "enc",
		Scopes:  []string{"/olcf.s3m.slurm.v0042.SlurmIndirect/GetJobs"},
	})

	conn := new(grpc.ClientConn)
	ts := buildToolSet(rt, rt.Methods, conn, cache.Vars)
	route, ok := findHTTPRoute(ts.HTTP, "/docs/lookup")
	if !ok {
		t.Fatal("expected docs lookup route to be registered")
	}

	handler := wrapStatelessDocRoute(route, rt, conn, cache)

	req := httptest.NewRequest(http.MethodPost, route.Path, strings.NewReader(`{"doc_id":"doc1"}`))
	req.Header.Set("Authorization", "Bearer jobs-token")

	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `doc_id "doc1" not found`) {
		t.Fatalf("expected hidden doc to look absent, got %q", rr.Body.String())
	}
}

func TestMakeStatelessOpenAPISpecHandlerScopesPathsByToken(t *testing.T) {
	file := slurmv0042pb.File_proto_slurm_v0042_slurm_proto
	service := file.Services().ByName("SlurmIndirect")
	getJobs := service.Methods().ByName("GetJobs")
	getNodes := service.Methods().ByName("GetNodes")

	rt := &runtime.Runtime{
		Methods: []proto.MethodInfo{
			{
				Service:  service,
				Method:   getJobs,
				ToolName: "slurm_get_jobs",
				Path:     "/jobs",
				Desc:     "Get jobs",
			},
			{
				Service:  service,
				Method:   getNodes,
				ToolName: "slurm_get_nodes",
				Path:     "/nodes",
				Desc:     "Get nodes",
			},
		},
	}

	cache := newTokenCache(nil)
	cache.storeRecord("jobs-token", auth.TokenRecord{
		Token:   "jobs-token",
		Project: "proj",
		Enclave: "enc",
		Scopes:  []string{"/olcf.s3m.slurm.v0042.SlurmIndirect/GetJobs"},
	})

	handler := makeStatelessOpenAPISpecHandler(rt, nil, cache)

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	req.Header.Set("Authorization", "Bearer jobs-token")

	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var spec map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &spec); err != nil {
		t.Fatalf("parse spec: %v", err)
	}

	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatalf("expected paths map, got %+v", spec["paths"])
	}
	if _, ok := paths["/jobs"]; !ok {
		t.Fatalf("expected allowed path in spec, got %+v", paths)
	}
	if _, ok := paths["/nodes"]; ok {
		t.Fatalf("unexpected forbidden path in spec: %+v", paths)
	}
}

func TestMakeStatelessOpenAPISpecHandlerFallsBackToAllPathsWhenPermissionsUnknown(t *testing.T) {
	file := slurmv0042pb.File_proto_slurm_v0042_slurm_proto
	service := file.Services().ByName("SlurmIndirect")
	getJobs := service.Methods().ByName("GetJobs")
	getNodes := service.Methods().ByName("GetNodes")

	rt := &runtime.Runtime{
		Methods: []proto.MethodInfo{
			{
				Service:  service,
				Method:   getJobs,
				ToolName: "slurm_get_jobs",
				Path:     "/jobs",
				Desc:     "Get jobs",
			},
			{
				Service:  service,
				Method:   getNodes,
				ToolName: "slurm_get_nodes",
				Path:     "/nodes",
				Desc:     "Get nodes",
			},
		},
	}

	handler := makeStatelessOpenAPISpecHandler(rt, nil, newTokenCache(nil))

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	req.Header.Set("Authorization", "Bearer unknown-token")

	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var spec map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &spec); err != nil {
		t.Fatalf("parse spec: %v", err)
	}

	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatalf("expected paths map, got %+v", spec["paths"])
	}

	for _, path := range []string{"/jobs", "/nodes"} {
		if _, ok := paths[path]; !ok {
			t.Fatalf("expected unknown-permissions spec to include %s, got %+v", path, paths)
		}
	}
}

func storageMethodInfo(t *testing.T, methodName string) proto.MethodInfo {
	t.Helper()

	service := storagepb.File_proto_storage_v1alpha_storage_proto.Services().ByName("BucketGateway")
	if service == nil {
		t.Fatal("storage service descriptor not found")
	}

	method := service.Methods().ByName(protoreflect.Name(methodName))
	if method == nil {
		t.Fatalf("storage method %q not found", methodName)
	}

	return proto.MethodInfo{
		File:     storagepb.File_proto_storage_v1alpha_storage_proto,
		Service:  service,
		Method:   method,
		ToolName: proto.ToolNameForMethod(storagepb.File_proto_storage_v1alpha_storage_proto, service, method),
		Path:     proto.BuildRESTPath("storage", "v1alpha", method),
		API:      "storage",
		Version:  "v1alpha",
	}
}
