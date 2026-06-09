package grpc

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

type MethodKey struct {
	ServiceFull string
	Method      string
}

//
// Interceptors

//nolint:prealloc
func makeAuthInterceptor(defaultToken string, debug bool) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context, method string, req, reply any,
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption,
	) error {
		token := defaultToken

		ctxToken, hasCtxToken := authTokenFromContext(ctx)
		if hasCtxToken {
			token = ctxToken
		}

		if token == "" {
			slog.Warn("gRPC call without auth token",
				"method", method,
				"hasDefaultToken", defaultToken != "",
				"hasContextToken", hasCtxToken)
		}

		if token != "" {
			ctx = metadata.AppendToOutgoingContext(ctx, "authorization", token)
		}

		if debug {
			md, _ := metadata.FromOutgoingContext(ctx)
			sanitizedMD := metadata.MD{}

			for k, v := range md {
				if k != "authorization" {
					sanitizedMD[k] = v
				} else if len(v) > 0 {
					sanitizedMD[k] = []string{truncateToken(v[0])}
				}
			}

			attrs := []any{"method", method, "headers", sanitizedMD}
			attrs = append(attrs, debugMessageAttrs(req)...)

			slog.Info("S3M gRPC request", attrs...)
		}

		err := invoker(ctx, method, req, reply, cc, opts...)

		if debug {
			attrs := []any{"method", method}
			attrs = append(attrs, debugMessageAttrs(reply)...)

			if err != nil {
				attrs = append(attrs, "err", err)
				slog.Error("S3M gRPC response (error)", attrs...)
			} else {
				slog.Info("S3M gRPC response", attrs...)
			}
		}

		return err
	}
}

//
// Connection management

func DialAndWait(
	ctx context.Context, target string, token string, timeout time.Duration, debug bool,
) (*grpc.ClientConn, error) {
	creds := credentials.NewTLS(&tls.Config{
		MinVersion: tls.VersionTLS12,
	})

	conn, err := grpc.NewClient(target,
		grpc.WithTransportCredentials(creds),
		grpc.WithUnaryInterceptor(makeAuthInterceptor(token, debug)),
	)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn.Connect()

	state := conn.GetState()

	for state != connectivity.Ready {
		if !conn.WaitForStateChange(ctx, state) {
			_ = conn.Close()

			return nil, fmt.Errorf("connect timeout: last state=%s", state)
		}

		state = conn.GetState()
	}

	return conn, nil
}

//
// RPC invocation

//nolint:funlen
func InvokeJSON(
	ctx context.Context,
	bodyJSON []byte,
	md protoreflect.MethodDescriptor,
	conn *grpc.ClientConn,
	key MethodKey,
	headerVals map[string]string,
	callTimeout time.Duration,
	debug bool,
) ([]byte, error) {
	inMsg := dynamicpb.NewMessage(md.Input())

	full := "/" + key.ServiceFull + "/" + key.Method

	if debug {
		slog.Info("S3M gRPC invoke JSON",
			"method", full,
			"request", string(bodyJSON),
			"headers", headerVals,
		)
	}

	if len(bodyJSON) > 0 {
		if err := protojson.Unmarshal(bodyJSON, inMsg); err != nil {
			return nil, fmt.Errorf("unmarshal input: %w", err)
		}
	}

	outMsg := dynamicpb.NewMessage(md.Output())

	if len(headerVals) > 0 {
		existingMD, _ := metadata.FromOutgoingContext(ctx)

		mdCopy := metadata.MD{}
		if existingMD != nil {
			mdCopy = existingMD.Copy()
		}

		for k, v := range headerVals {
			mdCopy.Set(k, v)
		}

		ctx = metadata.NewOutgoingContext(ctx, mdCopy)
	}

	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	if err := conn.Invoke(callCtx, full, inMsg, outMsg); err != nil {
		return nil, fmt.Errorf("grpc invoke %s: %w", full, err)
	}

	m := protojson.MarshalOptions{EmitUnpopulated: true, UseProtoNames: false}

	respJSON, err := m.Marshal(outMsg)
	if err != nil {
		return nil, fmt.Errorf("marshal output: %w", err)
	}

	if debug {
		slog.Info("S3M gRPC invoke JSON response", "method", full, "response", string(respJSON))
	}

	return respJSON, nil
}

//
// Auth

type authTokenContextKey struct{}

// ContextWithAuthToken returns a context that carries the S3M auth token.
func ContextWithAuthToken(ctx context.Context, token string) context.Context {
	if ctx == nil || token == "" {
		return ctx
	}

	return context.WithValue(ctx, authTokenContextKey{}, token)
}

func authTokenFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}

	v := ctx.Value(authTokenContextKey{})
	if v == nil {
		return "", false
	}

	token, ok := v.(string)
	if !ok || token == "" {
		return "", false
	}

	return token, true
}

// AuthTokenFromContext retrieves the S3M auth token stored in ctx, if present.
func AuthTokenFromContext(ctx context.Context) (string, bool) {
	return authTokenFromContext(ctx)
}

func TokenFromAuthorizationHeader(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}

	const bearerPrefix = "bearer "

	if len(header) > len(bearerPrefix) && strings.EqualFold(header[:len(bearerPrefix)], bearerPrefix) {
		return strings.TrimSpace(header[len(bearerPrefix):])
	}

	return header
}

func truncateToken(token string) string {
	if len(token) <= 16 {
		return token
	}

	return token[:8] + "..." + token[len(token)-8:]
}

func debugMessageAttrs(msg any) []any {
	if msg == nil {
		return []any{"message_type", "<nil>"}
	}

	protoMsg, ok := msg.(proto.Message)
	if !ok || protoMsg == nil {
		return []any{"message_type", fmt.Sprintf("%T", msg)}
	}

	desc := protoMsg.ProtoReflect().Descriptor()
	if desc == nil {
		return []any{"message_type", fmt.Sprintf("%T", msg)}
	}

	return []any{
		"message_type", string(desc.FullName()),
		"message_size", proto.Size(protoMsg),
	}
}
