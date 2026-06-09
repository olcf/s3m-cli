package proto

import (
	"slices"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

const ToolNameMaxLen = 64

//
// Package parsing

func ParsePackageInfo(pkg string) (api string, version string) {
	segments := slices.Collect(strings.SplitSeq(pkg, "."))
	return parseAPIVersion(segments)
}

//
// Version detection

func isVersionSegment(seg string) bool {
	if len(seg) < 2 || seg[0] != 'v' {
		return false
	}

	c := seg[1]

	return c >= '0' && c <= '9'
}

//
// Path and tool name generation

func BuildRESTPath(api, version string, md protoreflect.MethodDescriptor) string {
	methodPath := strings.ToLower(string(md.Name()))
	return "/" + strings.ToLower(api) + "/" + version + "/" + methodPath
}

func ToolNameForMethod(
	fd protoreflect.FileDescriptor, sd protoreflect.ServiceDescriptor, md protoreflect.MethodDescriptor,
) string {
	pkg := string(fd.Package())
	segments := slices.Collect(strings.SplitSeq(pkg, "."))
	_, version := parseAPIVersion(segments)

	serviceName := string(sd.Name())
	methodName := string(md.Name())

	var base string
	if version != "" {
		base = serviceName + "_" + version + "_" + methodName
	} else {
		base = serviceName + "_" + methodName
	}

	return sanitizeToolName(base)
}

func sanitizeToolName(name string) string {
	var b strings.Builder

	b.Grow(len(name))

	for _, r := range name {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}

	out := b.String()

	if len(out) > ToolNameMaxLen {
		out = out[len(out)-ToolNameMaxLen:]
	}

	return out
}

func parseAPIVersion(segments []string) (api string, version string) {
	api = "default"
	version = ""

	idx := lastVersionIndex(segments)
	if idx == -1 {
		return
	}

	version = segments[idx]

	if idx > 0 {
		api = segments[idx-1]
	}

	return
}

func lastVersionIndex(segments []string) int {
	idx := -1

	for i, seg := range segments {
		if isVersionSegment(seg) {
			idx = i
		}
	}

	return idx
}
