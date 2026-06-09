package toolset

import (
	"io"
	"log/slog"
	"maps"
	"net/http"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"

	buildinfo "github.com/olcf/s3m-cli"
	grpcclient "github.com/olcf/s3m-cli/internal/grpc"
	"github.com/olcf/s3m-cli/internal/proto"
)

func GenerateOpenAPISpec(paths []OpenAPIPathSpec) map[string]any {
	spec := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   "S3M gRPC OpenAPI Proxy",
			"version": buildinfo.Version,
		},
		"paths": map[string]any{},
	}

	pathMap, ok := spec["paths"].(map[string]any)
	if !ok {
		return spec
	}

	for _, p := range paths {
		if existing, ok := pathMap[p.Path]; ok {
			existingMap, _ := existing.(map[string]any)
			maps.Copy(existingMap, p.PathItem)

			continue
		}

		pathMap[p.Path] = p.PathItem
	}

	return spec
}

func RegisterOpenAPIHandlers(mux *http.ServeMux, routes []HTTPRouteSpec) {
	for _, route := range routes {
		mux.HandleFunc(route.Path, route.Handler)
	}

	if len(routes) > 0 {
		slog.Info("OpenAPI handlers registered", "count", len(routes))
	}
}

func makeOpenAPIHandler(
	key grpcclient.MethodKey,
	md protoreflect.MethodDescriptor,
	conn *grpc.ClientConn,
	headers []proto.HeaderParam,
	callTimeout time.Duration,
	debug bool,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			WriteCORSHeaders(w, r)
			w.WriteHeader(http.StatusNoContent)

			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed, use POST", http.StatusMethodNotAllowed)
			return
		}

		WriteCORSHeaders(w, r)

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body: "+err.Error(), http.StatusBadRequest)
			return
		}

		defer func() {
			if err := r.Body.Close(); err != nil {
				slog.Warn("failed to close request body", "error", err)
			}
		}()

		bodyJSON, headerVals, err := proto.ExtractHeadersAndBody(body, headers)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		respJSON, err := grpcclient.InvokeJSON(r.Context(), bodyJSON, md, conn, key, headerVals, callTimeout, debug)
		if err != nil {
			http.Error(w, err.Error(), httpStatusForGRPCError(err))
			return
		}

		w.Header().Set("Content-Type", "application/json")

		if _, err := w.Write(respJSON); err != nil {
			slog.Warn("failed to write response", "error", err)
		}
	}
}

func WriteCORSHeaders(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Vary", "Origin")

	w.Header().Set("Access-Control-Allow-Credentials", "true")

	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}

func httpStatusForGRPCError(err error) int {
	switch status.Code(err) {
	case codes.OK:
		return http.StatusOK
	case codes.Canceled:
		return http.StatusRequestTimeout
	case codes.InvalidArgument, codes.FailedPrecondition, codes.OutOfRange:
		return http.StatusBadRequest
	case codes.Unknown, codes.Internal, codes.DataLoss:
		return http.StatusInternalServerError
	case codes.Unauthenticated:
		return http.StatusUnauthorized
	case codes.PermissionDenied:
		return http.StatusForbidden
	case codes.NotFound:
		return http.StatusNotFound
	case codes.AlreadyExists, codes.Aborted:
		return http.StatusConflict
	case codes.ResourceExhausted:
		return http.StatusTooManyRequests
	case codes.Unimplemented:
		return http.StatusNotImplemented
	case codes.Unavailable:
		return http.StatusServiceUnavailable
	case codes.DeadlineExceeded:
		return http.StatusGatewayTimeout
	default:
		return http.StatusInternalServerError
	}
}
