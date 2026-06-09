package grpc

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

func TestInvokeJSON(t *testing.T) {
	md := buildTestMethodDescriptor(t)
	reqDesc := md.Input()
	respDesc := md.Output()

	conn := newBufconnTestClient(t, md, func(ctx context.Context, in *dynamicpb.Message) (proto.Message, error) {
		mdHeaders, _ := metadata.FromIncomingContext(ctx)
		if got := mdHeaders.Get("custom-header"); len(got) != 1 || got[0] != "hval" {
			return nil, status.Error(codes.Internal, "missing metadata")
		}

		val := in.Get(reqDesc.Fields().ByName("msg")).String()
		out := dynamicpb.NewMessage(respDesc)
		out.Set(respDesc.Fields().ByName("reply"), protoreflect.ValueOfString("ack:"+val))

		return out, nil
	})

	bodyJSON := []byte(`{"msg":"hi"}`)

	respJSON, err := InvokeJSON(context.Background(), bodyJSON, md, conn,
		MethodKey{ServiceFull: "test.v1.Echo", Method: "Ping"},
		map[string]string{"custom-header": "hval"},
		time.Second,
		false,
	)
	if err != nil {
		t.Fatalf("InvokeJSON: %v", err)
	}

	var resp map[string]string
	if err := json.Unmarshal(respJSON, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp["reply"] != "ack:hi" {
		t.Fatalf("unexpected response: %v", resp)
	}
}

func TestInvokeJSONRejectsInvalidInput(t *testing.T) {
	md := buildTestMethodDescriptor(t)

	_, err := InvokeJSON(
		context.Background(),
		[]byte(`{"msg":`),
		md,
		nil,
		MethodKey{ServiceFull: "test.v1.Echo", Method: "Ping"},
		nil,
		time.Second,
		false,
	)
	if err == nil || !strings.Contains(err.Error(), "unmarshal input") {
		t.Fatalf("expected unmarshal input error, got %v", err)
	}
}

func TestInvokeJSONWrapsRPCError(t *testing.T) {
	md := buildTestMethodDescriptor(t)
	conn := newBufconnTestClient(t, md, func(context.Context, *dynamicpb.Message) (proto.Message, error) {
		return nil, status.Error(codes.PermissionDenied, "denied")
	})

	_, err := InvokeJSON(
		context.Background(),
		[]byte(`{"msg":"hi"}`),
		md,
		conn,
		MethodKey{ServiceFull: "test.v1.Echo", Method: "Ping"},
		nil,
		time.Second,
		false,
	)
	if err == nil {
		t.Fatal("expected invoke error")
	}
	if !strings.Contains(err.Error(), "grpc invoke /test.v1.Echo/Ping") {
		t.Fatalf("expected wrapped method path, got %v", err)
	}
	if !strings.Contains(err.Error(), "denied") {
		t.Fatalf("expected underlying gRPC error, got %v", err)
	}
}

func TestDialAndWaitTimesOut(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	addr := lis.Addr().String()
	if err := lis.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	_, err = DialAndWait(context.Background(), addr, "", 20*time.Millisecond, false)
	if err == nil {
		t.Fatal("expected dial timeout")
	}
	if !strings.Contains(err.Error(), "connect timeout") {
		t.Fatalf("expected connect timeout error, got %v", err)
	}
}

func TestMakeAuthInterceptorPrefersContextToken(t *testing.T) {
	interceptor := makeAuthInterceptor("default-token", false)

	var authHeader []string

	err := interceptor(
		ContextWithAuthToken(context.Background(), "context-token"),
		"/test.v1.Echo/Ping",
		nil,
		nil,
		nil,
		func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
			md, _ := metadata.FromOutgoingContext(ctx)
			authHeader = md.Get("authorization")
			return nil
		},
	)
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
	if len(authHeader) != 1 || authHeader[0] != "context-token" {
		t.Fatalf("expected context token to win, got %v", authHeader)
	}
}

func newBufconnTestClient(
	t *testing.T,
	md protoreflect.MethodDescriptor,
	handler func(context.Context, *dynamicpb.Message) (proto.Message, error),
) *grpc.ClientConn {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	t.Cleanup(func() { _ = lis.Close() })

	srv := grpc.NewServer()
	t.Cleanup(srv.Stop)

	srv.RegisterService(&grpc.ServiceDesc{
		ServiceName: "test.v1.Echo",
		HandlerType: (*struct{})(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "Ping",
			Handler: func(_ any, ctx context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
				in := dynamicpb.NewMessage(md.Input())
				if err := dec(in); err != nil {
					return nil, err
				}

				return handler(ctx, in)
			},
		}},
	}, nil)

	go func() {
		_ = srv.Serve(lis)
	}()

	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return conn
}

func buildTestMethodDescriptor(t *testing.T) protoreflect.MethodDescriptor {
	t.Helper()

	file := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("echo.proto"),
		Package: proto.String("test.v1"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("PingRequest"),
				Field: []*descriptorpb.FieldDescriptorProto{{
					Name:   proto.String("msg"),
					Number: proto.Int32(1),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				}},
			},
			{
				Name: proto.String("PingResponse"),
				Field: []*descriptorpb.FieldDescriptorProto{{
					Name:   proto.String("reply"),
					Number: proto.Int32(1),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				}},
			},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("Echo"),
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name:       proto.String("Ping"),
				InputType:  proto.String(".test.v1.PingRequest"),
				OutputType: proto.String(".test.v1.PingResponse"),
			}},
		}},
	}

	files, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{file}})
	if err != nil {
		t.Fatalf("NewFiles: %v", err)
	}

	fd, err := files.FindFileByPath("echo.proto")
	if err != nil {
		t.Fatalf("FindFileByPath: %v", err)
	}

	method := fd.Services().ByName("Echo").Methods().ByName("Ping")
	if method == nil {
		t.Fatal("method descriptor not found")
	}

	return method
}
