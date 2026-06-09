package permissions

import (
	"strings"

	"github.com/olcf/s3m-cli/internal/proto"
)

type Snapshot struct {
	Known   bool
	Raw     []string
	allowed map[string]struct{}
}

// publicAPIs are reachable by every request, including unauthenticated ones,
// because SEAS bypasses auth for them. Keep in sync with e.g. SEAS bypass_auth.
var publicAPIs = map[string]struct{}{
	"status": {},
}

func alwaysAllowed(m proto.MethodInfo) bool {
	_, ok := publicAPIs[m.API]
	return ok
}

func New(scopes []string, known bool) Snapshot {
	allowed := make(map[string]struct{}, len(scopes))

	for _, s := range scopes {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}

		allowed[strings.ToLower(s)] = struct{}{}
	}

	if known && !hasUsableScope(allowed) {
		known = false
	}

	return Snapshot{
		Known:   known,
		Raw:     scopes,
		allowed: allowed,
	}
}

func (s Snapshot) Can(m proto.MethodInfo) bool {
	if alwaysAllowed(m) {
		return true
	}

	if !s.Known {
		return false
	}

	if len(s.allowed) == 0 {
		return false
	}

	grpcPath := grpcPathForMethod(m)

	for scope := range s.allowed {
		if matchScope(scope, grpcPath) {
			return true
		}
	}

	return false
}

func (s Snapshot) Label(m proto.MethodInfo) string {
	if s.Can(m) {
		return "access allowed"
	}

	if !s.Known {
		return "access unknown"
	}

	return "no access"
}

func grpcPathForMethod(m proto.MethodInfo) string {
	if m.Service == nil || m.Method == nil {
		return ""
	}

	return strings.ToLower("/" + string(m.Service.FullName()) + "/" + string(m.Method.Name()))
}

func matchScope(scope, grpcPath string) bool {
	if scope == "" || grpcPath == "" {
		return false
	}

	if scope == "*" {
		return true
	}

	if scope == grpcPath {
		return true
	}

	if prefix, ok := strings.CutSuffix(scope, "/*"); ok {
		return strings.HasPrefix(grpcPath, prefix+"/")
	}

	return false
}

func hasUsableScope(scopes map[string]struct{}) bool {
	for scope := range scopes {
		if usableScope(scope) {
			return true
		}
	}

	return false
}

func usableScope(scope string) bool {
	if scope == "*" {
		return true
	}

	if !strings.HasPrefix(scope, "/") {
		return false
	}

	service, method, ok := strings.Cut(strings.TrimPrefix(scope, "/"), "/")
	if !ok || service == "" || method == "" {
		return false
	}

	return !strings.Contains(method, "/")
}
