package proto

import (
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/olcf/s3m-cli/internal/headermap"
)

type MethodInfo struct {
	File     protoreflect.FileDescriptor
	Service  protoreflect.ServiceDescriptor
	Method   protoreflect.MethodDescriptor
	Headers  []HeaderParam
	ToolName string
	Path     string
	Desc     string
	API      string
	Version  string
}

func CollectMethods(files *protoregistry.Files, enclave string, resolver headermap.Resolver) []MethodInfo {
	if resolver == nil {
		resolver = headermap.DefaultResolver()
	}

	var methods []MethodInfo

	files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		api, version := ParsePackageInfo(string(fd.Package()))
		services := fd.Services()

		for i := range services.Len() {
			sd := services.Get(i)

			for j := range sd.Methods().Len() {
				md := sd.Methods().Get(j)

				if md.IsStreamingClient() || md.IsStreamingServer() {
					continue
				}

				toolName := ToolNameForMethod(fd, sd, md)
				path := BuildRESTPath(api, version, md)
				headers := HeaderParamsForMethod(enclave, resolver, sd, md)
				desc := "Call gRPC method " + string(sd.FullName()) + "/" + string(md.Name())

				methods = append(methods, MethodInfo{
					File:     fd,
					Service:  sd,
					Method:   md,
					Headers:  headers,
					ToolName: toolName,
					Path:     path,
					Desc:     desc,
					API:      api,
					Version:  version,
				})
			}
		}

		return true
	})

	return methods
}
