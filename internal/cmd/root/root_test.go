package root

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/olcf/s3m-cli/internal/auth"
	"github.com/olcf/s3m-cli/internal/completion"
	"github.com/olcf/s3m-cli/internal/runtime"
)

func TestFishCompletionScriptDelegatesToGenerateFlag(t *testing.T) {
	script := completion.GenerateFish("s3mcli")

	if !strings.Contains(script, "--generate-shell-completion") {
		t.Fatalf("expected script to invoke --generate-shell-completion, got: %s", script)
	}

	if !strings.Contains(script, "complete -c s3mcli") {
		t.Fatalf("expected script to register completion for app name, got: %s", script)
	}
}

func TestBuildDefaultsBlankAppName(t *testing.T) {
	rt := &runtime.Runtime{State: auth.NewState()}

	cmd := Build(rt, "")
	if cmd.Name != "s3m" {
		t.Fatalf("expected default app name s3m, got %q", cmd.Name)
	}
}

func TestBuildNormalizesAppNameAndCommands(t *testing.T) {
	rt := &runtime.Runtime{State: auth.NewState()}

	cmd := Build(rt, "/usr/local/bin/s3mcli")
	if cmd.Name != "s3mcli" {
		t.Fatalf("expected normalized app name, got %q", cmd.Name)
	}

	wantFlags := map[string]struct{}{
		"debug":              {},
		"grpc-target":        {},
		"ignore-permissions": {},
	}
	for _, flag := range cmd.Flags {
		for _, name := range flag.Names() {
			delete(wantFlags, name)
		}
	}
	if len(wantFlags) > 0 {
		t.Fatalf("missing root flags: %+v", wantFlags)
	}

	wantCommands := map[string]struct{}{
		"login":   {},
		"auth":    {},
		"exec":    {},
		"storage": {},
		"mcp":     {},
		"openapi": {},
	}
	for _, sub := range cmd.Commands {
		delete(wantCommands, sub.Name)
	}
	if len(wantCommands) > 0 {
		t.Fatalf("missing root commands: %+v", wantCommands)
	}
}

func TestHelpStillWorksWithMalformedAuthState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte("{bad json"), 0o600); err != nil {
		t.Fatalf("write auth state: %v", err)
	}

	t.Setenv("S3M_AUTH_PATH", path)

	rt, err := runtime.Bootstrap()
	if err != nil {
		t.Fatalf("Bootstrap should ignore auth-state errors: %v", err)
	}

	cmd := Build(rt, "s3m")

	var out bytes.Buffer
	cmd.Writer = &out
	cmd.ErrWriter = &out
	cmd.ExitErrHandler = func(context.Context, *cli.Command, error) {}

	if err := cmd.Run(context.Background(), []string{"s3m", "--help"}); err != nil {
		t.Fatalf("help should still run: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "Interact with S3M APIs and MCP/OpenAPI servers") {
		t.Fatalf("expected root help output, got: %s", output)
	}
	if !strings.Contains(output, "auth") {
		t.Fatalf("expected auth command in help output, got: %s", output)
	}
}

func TestAuthStatusReportsStateLoadErrorAfterDispatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte("{bad json"), 0o600); err != nil {
		t.Fatalf("write auth state: %v", err)
	}

	t.Setenv("S3M_AUTH_PATH", path)

	rt, err := runtime.Bootstrap()
	if err != nil {
		t.Fatalf("Bootstrap should ignore auth-state errors: %v", err)
	}

	cmd := Build(rt, "s3m")

	var out bytes.Buffer
	cmd.Writer = &out
	cmd.ErrWriter = &out
	cmd.ExitErrHandler = func(context.Context, *cli.Command, error) {}

	err = cmd.Run(context.Background(), []string{"s3m", "auth", "status"})
	if err == nil {
		t.Fatal("expected auth status to report the state-load error")
	}
	if !strings.Contains(err.Error(), "load auth state") {
		t.Fatalf("expected load auth state error, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "parse state") {
		t.Fatalf("expected parse state detail, got %q", err.Error())
	}
	if strings.Contains(err.Error(), "unable to start") {
		t.Fatalf("expected dispatched command error, got %q", err.Error())
	}
}
