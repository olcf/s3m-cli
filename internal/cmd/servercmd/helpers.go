package servercmd

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/urfave/cli/v3"
	"google.golang.org/grpc"

	"github.com/olcf/s3m-cli/internal/cmd"
	grpcclient "github.com/olcf/s3m-cli/internal/grpc"
	"github.com/olcf/s3m-cli/internal/permissions"
	"github.com/olcf/s3m-cli/internal/proto"
	"github.com/olcf/s3m-cli/internal/runtime"
	"github.com/olcf/s3m-cli/internal/toolset"
)

func buildToolSet(
	rt *runtime.Runtime,
	allowed []proto.MethodInfo,
	conn *grpc.ClientConn,
	resolver toolset.DocVariableProvider,
) *toolset.ToolSet {
	ts := toolset.New()

	// Filter out BucketGateway methods; replaced by custom storage tools
	filtered := filterBucketGatewayMethods(allowed)
	grpcTS := toolset.BuildGRPCToolSet(filtered, conn, cmd.GRPCCallTimeout, rt.Debug)
	ts.Merge(grpcTS)

	storageTS := toolset.BuildStorageToolSet(conn, cmd.GRPCCallTimeout, rt.Debug, allowed)
	if storageTS != nil {
		slog.Debug("buildToolSet: storage tools", "count", len(storageTS.MCP))
		ts.Merge(storageTS)
	}

	slog.Debug("buildToolSet: gRPC tools", "count", len(grpcTS.MCP))
	slog.Debug("buildToolSet: total MCP tools", "count", len(ts.MCP))

	if store := rt.GetDocs(); store != nil {
		toolset.AnnotateToolsWithDocs(ts, store)
		ts.Merge(toolset.BuildDocToolSet(rt.GetDocs, resolver, visibleToolNames(ts.MCP)))
	}

	return ts
}

func filterBucketGatewayMethods(methods []proto.MethodInfo) []proto.MethodInfo {
	const bucketGatewayService = "olcf.s3m.storage.v1alpha.BucketGateway"

	filtered := make([]proto.MethodInfo, 0, len(methods))

	for _, m := range methods {
		if string(m.Service.FullName()) == bucketGatewayService {
			continue
		}

		filtered = append(filtered, m)
	}

	return filtered
}

func filterAllowed(perms permissions.Snapshot, methods []proto.MethodInfo) []proto.MethodInfo {
	if !perms.Known {
		return append([]proto.MethodInfo(nil), methods...)
	}

	var allowed []proto.MethodInfo

	for _, m := range methods {
		if perms.Can(m) {
			allowed = append(allowed, m)
		}
	}

	return allowed
}

func visibleToolNames(specs []toolset.MCPToolSpec) map[string]struct{} {
	visible := make(map[string]struct{}, len(specs))

	for _, spec := range specs {
		if spec.Tool == nil {
			continue
		}

		name := strings.ToLower(strings.TrimSpace(spec.Tool.Name))
		if name == "" {
			continue
		}

		visible[name] = struct{}{}
	}

	return visible
}

func hasAllowedMethodNames(methods []proto.MethodInfo, required ...string) bool {
	if len(required) == 0 {
		return true
	}

	allowed := make(map[string]struct{}, len(methods))

	for _, m := range methods {
		if m.Method == nil {
			continue
		}

		allowed[string(m.Method.Name())] = struct{}{}
	}

	for _, name := range required {
		if _, ok := allowed[name]; !ok {
			return false
		}
	}

	return true
}

func closeConn(conn *grpc.ClientConn) {
	if conn == nil {
		return
	}

	if err := conn.Close(); err != nil {
		slog.Error("failed to close gRPC connection", "error", err)
	}
}

func prepareConnection(ctx context.Context, rt *runtime.Runtime) ([]proto.MethodInfo, *grpc.ClientConn, error) {
	if err := rt.EnsureState(); err != nil {
		return nil, nil, cli.Exit(err.Error(), 1)
	}

	tokenRec, ok := rt.CurrentToken()
	if !ok {
		return nil, nil, cli.Exit("no active token; login with `s3m login token`", 1)
	}

	perms := rt.PermissionSnapshot()
	allowed := filterAllowed(perms, rt.Methods)

	conn, err := grpcclient.DialAndWait(ctx, rt.Target, tokenRec.Token, cmd.GRPCConnectTimeout, rt.Debug)
	if err != nil {
		return nil, nil, cli.Exit(err.Error(), 1)
	}

	return allowed, conn, nil
}

func prepareStatelessConnection(
	ctx context.Context,
	rt *runtime.Runtime,
) ([]proto.MethodInfo, *grpc.ClientConn, error) {
	allowed := rt.Methods

	conn, err := grpcclient.DialAndWait(ctx, rt.Target, "", cmd.GRPCConnectTimeout, rt.Debug)
	if err != nil {
		return nil, nil, cli.Exit(err.Error(), 1)
	}

	return allowed, conn, nil
}

func wrapWithAuthTokenExtraction(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			handler.ServeHTTP(w, r)
			return
		}

		token := grpcclient.TokenFromAuthorizationHeader(r.Header.Get("Authorization"))

		logAuthTokenExtraction(r, token)

		if token == "" {
			logMissingAuthorization(r)
			http.Error(w, "missing Authorization header", http.StatusUnauthorized)

			return
		}

		ctx := grpcclient.ContextWithAuthToken(r.Context(), token)
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}

func registerStatelessOpenAPIHandlers(
	mux *http.ServeMux,
	routes []toolset.HTTPRouteSpec,
	rt *runtime.Runtime,
	conn *grpc.ClientConn,
	cache *tokenCache,
) {
	for _, route := range routes {
		if isDocsRoute(route.Path) {
			mux.HandleFunc(route.Path, wrapStatelessDocRoute(route, rt, conn, cache))
			continue
		}

		mux.HandleFunc(route.Path, wrapStatelessOpenAPIRoute(route, rt, cache))
	}

	if len(routes) > 0 {
		slog.Info("OpenAPI handlers registered", "count", len(routes))
	}
}

func isDocsRoute(path string) bool {
	return strings.HasPrefix(path, "/docs/")
}

func wrapStatelessDocRoute(
	route toolset.HTTPRouteSpec,
	rt *runtime.Runtime,
	conn *grpc.ClientConn,
	cache *tokenCache,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			route.Handler(w, r)

			return
		}

		token, ok := extractStatelessAuth(w, r)
		if !ok {
			return
		}

		ctx := grpcclient.ContextWithAuthToken(r.Context(), token)
		allowed := filterAllowed(cache.GetPermissions(ctx, token), rt.Methods)
		current := buildToolSet(rt, allowed, conn, cache.Vars)

		docsRoute, ok := findHTTPRoute(current.HTTP, route.Path)
		if !ok {
			http.NotFound(w, r)

			return
		}

		docsRoute.Handler(w, r.WithContext(ctx))
	}
}

func findHTTPRoute(routes []toolset.HTTPRouteSpec, path string) (toolset.HTTPRouteSpec, bool) {
	for _, route := range routes {
		if route.Path == path {
			return route, true
		}
	}

	return toolset.HTTPRouteSpec{}, false
}

func wrapStatelessOpenAPIRoute(
	route toolset.HTTPRouteSpec,
	rt *runtime.Runtime,
	cache *tokenCache,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if handleStatelessOpenAPIOptions(w, r, route, rt, cache) {
			return
		}

		token, ok := extractStatelessAuth(w, r)
		if !ok {
			return
		}

		ctx := grpcclient.ContextWithAuthToken(r.Context(), token)
		perms := cache.GetPermissions(ctx, token)

		if !routeAllowedForToken(route, perms, rt) {
			if len(route.RequiredMethodNames) > 0 {
				http.NotFound(w, r)
			} else {
				http.Error(w, "forbidden", http.StatusForbidden)
			}

			return
		}

		route.Handler(w, r.WithContext(ctx))
	}
}

func handleStatelessOpenAPIOptions(
	w http.ResponseWriter,
	r *http.Request,
	route toolset.HTTPRouteSpec,
	rt *runtime.Runtime,
	cache *tokenCache,
) bool {
	if r.Method != http.MethodOptions {
		return false
	}

	if len(route.RequiredMethodNames) > 0 {
		token := grpcclient.TokenFromAuthorizationHeader(r.Header.Get("Authorization"))
		if token != "" {
			ctx := grpcclient.ContextWithAuthToken(r.Context(), token)
			perms := cache.GetPermissions(ctx, token)

			if !routeAllowedForToken(route, perms, rt) {
				http.NotFound(w, r)

				return true
			}

			route.Handler(w, r.WithContext(ctx))

			return true
		}
	}

	route.Handler(w, r)

	return true
}

func routeAllowedForToken(route toolset.HTTPRouteSpec, perms permissions.Snapshot, rt *runtime.Runtime) bool {
	if !route.RequiresPermission {
		return true
	}

	if len(route.RequiredMethodNames) > 0 {
		if rt == nil {
			return false
		}

		allowed := filterAllowed(perms, rt.Methods)

		return hasAllowedMethodNames(allowed, route.RequiredMethodNames...)
	}

	if !perms.Known {
		return true
	}

	return perms.Can(route.Method)
}

func makeStatelessOpenAPISpecHandler(
	rt *runtime.Runtime,
	conn *grpc.ClientConn,
	cache *tokenCache,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			toolset.WriteCORSHeaders(w, r)
			w.WriteHeader(http.StatusNoContent)

			return
		}

		token, ok := extractStatelessAuth(w, r)
		if !ok {
			return
		}

		ctx := grpcclient.ContextWithAuthToken(r.Context(), token)
		allowed := filterAllowed(cache.GetPermissions(ctx, token), rt.Methods)
		ts := buildToolSet(rt, allowed, conn, cache.Vars)
		spec := toolset.GenerateOpenAPISpec(ts.OpenAPI)

		toolset.WriteCORSHeaders(w, r)
		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(spec); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func extractStatelessAuth(w http.ResponseWriter, r *http.Request) (string, bool) {
	token := grpcclient.TokenFromAuthorizationHeader(r.Header.Get("Authorization"))
	if token == "" {
		http.Error(w, "missing Authorization header", http.StatusUnauthorized)

		return "", false
	}

	return token, true
}
