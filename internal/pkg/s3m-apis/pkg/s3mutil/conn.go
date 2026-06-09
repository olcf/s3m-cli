package s3mutil

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

// S3MConnOptions defines configuration options for creating an S3M connection.
type S3MConnOptions struct {
	TLSConfig          *tls.Config
	Metadata           map[string]string
	UnaryInterceptors  []grpc.UnaryClientInterceptor
	StreamInterceptors []grpc.StreamClientInterceptor
}

func defaultS3MConnOptions() *S3MConnOptions {
	return &S3MConnOptions{
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
		Metadata: make(map[string]string),
	}
}

func unaryCtxInjector(vars map[string]string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		md := metadata.New(vars)

		if existing, ok := metadata.FromOutgoingContext(ctx); ok {
			md = metadata.Join(existing, md)
		}

		ctxWithVars := metadata.NewOutgoingContext(ctx, md)

		return invoker(ctxWithVars, method, req, reply, cc, opts...)
	}
}

func streamCtxInjector(vars map[string]string) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		md := metadata.New(vars)

		if existing, ok := metadata.FromOutgoingContext(ctx); ok {
			md = metadata.Join(existing, md)
		}

		ctxWithVars := metadata.NewOutgoingContext(ctx, md)

		return streamer(ctxWithVars, desc, cc, method, opts...)
	}
}

// NewS3MConnWithOpts creates a new S3M gRPC client connection with custom options.
func NewS3MConnWithOpts(endpoint, token string, opts *S3MConnOptions) (*grpc.ClientConn, error) {
	if opts == nil {
		return nil, fmt.Errorf("NewS3MConnWithOpts: opts is nil")
	}

	tlsCfg := opts.TLSConfig
	if tlsCfg == nil {
		tlsCfg = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	//
	// Parse endpoint
	if strings.HasPrefix(endpoint, "https://") {
		endpoint = endpoint[len("https://"):]
	}
	endpoint = strings.TrimRight(endpoint, "/")

	var host string
	var port int

	if strings.Contains(endpoint, ":") {
		parts := strings.Split(endpoint, ":")
		host = strings.Join(parts[:len(parts)-1], ":")
		var err error
		_, err = fmt.Sscanf(parts[len(parts)-1], "%d", &port)
		if err != nil {
			return nil, fmt.Errorf("failed to parse port: %w", err)
		}
	} else {
		host = endpoint
		port = 443
	}

	//
	// Prepare metadata
	vars := map[string]string{
		"Authorization": token,
	}

	for k, v := range opts.Metadata {
		vars[k] = v
	}

	//
	// Build dial opts
	creds := credentials.NewTLS(tlsCfg)

	unaryInterceptors := []grpc.UnaryClientInterceptor{unaryCtxInjector(vars)}
	streamInterceptors := []grpc.StreamClientInterceptor{streamCtxInjector(vars)}

	unaryInterceptors = append(unaryInterceptors, opts.UnaryInterceptors...)
	streamInterceptors = append(streamInterceptors, opts.StreamInterceptors...)

	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		grpc.WithChainUnaryInterceptor(unaryInterceptors...),
		grpc.WithChainStreamInterceptor(streamInterceptors...),
	}

	//
	// Build client
	conn, err := grpc.NewClient(
		fmt.Sprintf("%s:%d", host, port),
		dialOpts...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection: %w", err)
	}

	return conn, nil
}

// NewS3MConn creates a new S3M gRPC client connection using default options.
func NewS3MConn(endpoint, token string) (*grpc.ClientConn, error) {
	return NewS3MConnWithOpts(endpoint, token, defaultS3MConnOptions())
}
