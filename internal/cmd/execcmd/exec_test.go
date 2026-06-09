package execcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	slurmv0042pb "github.com/olcf/s3m-apis/slurm/v0042"
	"github.com/urfave/cli/v3"

	"github.com/olcf/s3m-cli/internal/auth"
	"github.com/olcf/s3m-cli/internal/headermap"
	"github.com/olcf/s3m-cli/internal/permissions"
	"github.com/olcf/s3m-cli/internal/proto"
	"github.com/olcf/s3m-cli/internal/runtime"
)

func TestExecCommandUsageIncludesRequiredHeader(t *testing.T) {
	file := slurmv0042pb.File_proto_slurm_v0042_slurm_proto
	service := file.Services().ByName("SlurmIndirect")
	md := service.Methods().ByName("GetJobs")

	headers := proto.HeaderParamsForMethod(headermap.DefaultEnclave, headermap.DefaultResolver(), service, md)

	method := proto.MethodInfo{
		File:    file,
		Service: service,
		Method:  md,
		Headers: headers,
		ToolName: proto.ToolNameForMethod(file,
			service, md),
		Path: proto.BuildRESTPath("slurm", "v0042", md),
	}
	perms := permissions.New(nil, false)
	aliases := map[string]int{slug(string(md.Name())): 1}
	cmd := buildExecMethodCommand(nil, method, perms, aliases)

	if !strings.Contains(cmd.Usage, "compute_resource") {
		t.Fatalf("expected usage to include header param; got %s", cmd.Usage)
	}
	if !strings.Contains(cmd.Usage, "[required]") {
		t.Fatalf("expected usage to mark required header; got %s", cmd.Usage)
	}
}

func TestPayloadFromArgsBuildsJSON(t *testing.T) {
	payload, err := payloadFromArgs([]string{
		"name=tester",
		"count=5",
		"flag=true",
		"obj={\"nested\":1}",
	})
	if err != nil {
		t.Fatalf("payloadFromArgs error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	if decoded["name"] != "tester" {
		t.Fatalf("expected name, got %v", decoded["name"])
	}
	if decoded["count"] != float64(5) || decoded["flag"] != true {
		t.Fatalf("unexpected typed values: %+v", decoded)
	}

	obj, ok := decoded["obj"].(map[string]any)
	if !ok || obj["nested"] != float64(1) {
		t.Fatalf("unexpected nested object: %+v", decoded["obj"])
	}
}

func TestPayloadFromArgsSupportsFilesAndHeaders(t *testing.T) {
	script := "#!/bin/bash\necho hi\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")

	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatalf("write temp script: %v", err)
	}

	payload, err := payloadFromArgs([]string{
		"compute_resource=defiant",
		"script=@" + path,
	})
	if err != nil {
		t.Fatalf("payloadFromArgs error: %v", err)
	}

	headers := []proto.HeaderParam{{
		Header:    "olcf-resource",
		ParamName: "compute_resource",
		Required:  true,
	}}

	body, hdrs, err := proto.ExtractHeadersAndBody(payload, headers)
	if err != nil {
		t.Fatalf("ExtractHeadersAndBody error: %v", err)
	}
	if hdrs["olcf-resource"] != "defiant" {
		t.Fatalf("expected header value, got %+v", hdrs)
	}

	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if decoded["script"] != script {
		t.Fatalf("expected script contents, got %q", decoded["script"])
	}
}

func TestPayloadFromArgsValidatesInput(t *testing.T) {
	if _, err := payloadFromArgs([]string{"invalid"}); err == nil {
		t.Fatal("expected error for missing '='")
	}

	if _, err := payloadFromArgs([]string{"=value"}); err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestPayloadFromArgsBuildsNestedJSON(t *testing.T) {
	t.Parallel()

	script := "#!/bin/bash\necho hi\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")

	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatalf("write temp script: %v", err)
	}

	payload, err := payloadFromArgs([]string{
		"compute_resource=defiant",
		"job.account=stf040-api",
		"job.current_working_directory=/lustre/polis/stf040/proj-shared/",
		"job.script=@" + path,
	})
	if err != nil {
		t.Fatalf("payloadFromArgs error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	if decoded["compute_resource"] != "defiant" {
		t.Fatalf("expected compute_resource, got %v", decoded["compute_resource"])
	}

	job, ok := decoded["job"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested job map, got %T", decoded["job"])
	}

	if job["account"] != "stf040-api" {
		t.Fatalf("expected account, got %v", job["account"])
	}
	if job["current_working_directory"] != "/lustre/polis/stf040/proj-shared/" {
		t.Fatalf("expected working directory, got %v", job["current_working_directory"])
	}
	if job["script"] != script {
		t.Fatalf("expected script contents, got %q", job["script"])
	}
}

func TestPayloadFromArgsSupportsSingleStdinValue(t *testing.T) {
	oldStdin := os.Stdin
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	defer func() {
		_ = reader.Close()
		os.Stdin = oldStdin
	}()

	if _, err := writer.WriteString("stdin payload"); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}

	os.Stdin = reader

	payload, err := payloadFromArgs([]string{"script=@-"})
	if err != nil {
		t.Fatalf("payloadFromArgs error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	if decoded["script"] != "stdin payload" {
		t.Fatalf("expected stdin payload, got %q", decoded["script"])
	}
}

func TestPayloadFromArgsRejectsRepeatedStdinValue(t *testing.T) {
	oldStdin := os.Stdin
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	defer func() {
		_ = reader.Close()
		os.Stdin = oldStdin
	}()

	if _, err := writer.WriteString("stdin payload"); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}

	os.Stdin = reader

	_, err = payloadFromArgs([]string{"first=@-", "second=@-"})
	if err == nil {
		t.Fatal("expected repeated @- to fail")
	}
	if !strings.Contains(err.Error(), "repeated @- is not allowed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecCommandActionPathErrors(t *testing.T) {
	t.Parallel()

	method := buildMethodInfo(t)

	tests := []struct {
		name      string
		rt        *runtime.Runtime
		args      []string
		wantErr   string
		wantNoErr string
	}{
		{
			name: "missing active token",
			rt: &runtime.Runtime{
				State:   auth.NewState(),
				Methods: []proto.MethodInfo{method},
			},
			args:    []string{"s3m", "exec", "slurm/v0043", "get-job", "job_id=123"},
			wantErr: "no active token",
		},
		{
			name: "known permission denial",
			rt:   newExecTestRuntime(t, method, []string{"/other.service/OtherMethod"}, true),
			args: []string{
				"s3m", "exec", "slurm/v0043", "get-job", "compute_resource=defiant", "job_id=123",
			},
			wantErr:   "permission denied",
			wantNoErr: "could not be confirmed",
		},
		{
			name: "unconfirmed permissions fall through",
			rt:   newExecTestRuntime(t, method, nil, false),
			args: []string{
				"s3m", "exec", "slurm/v0043", "get-job", "--data", "{}", "job_id=123",
			},
			wantErr:   "provide either --data or param=value arguments, not both",
			wantNoErr: "permission",
		},
		{
			name: "mixed data and args",
			rt:   newExecTestRuntime(t, method, []string{"*"}, true),
			args: []string{
				"s3m", "exec", "slurm/v0043", "get-job", "--data", "{}", "job_id=123",
			},
			wantErr: "provide either --data or param=value arguments, not both",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			cmd := &cli.Command{
				Name:           "s3m",
				Commands:       []*cli.Command{BuildExecCommand(tt.rt)},
				Writer:         &out,
				ErrWriter:      &out,
				ExitErrHandler: func(context.Context, *cli.Command, error) {},
			}

			err := cmd.Run(context.Background(), tt.args)
			if err == nil {
				t.Fatal("expected command error")
			}

			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}

			if tt.wantNoErr != "" && strings.Contains(err.Error(), tt.wantNoErr) {
				t.Fatalf("expected error to omit %q, got %q", tt.wantNoErr, err.Error())
			}
		})
	}
}

func TestSlugHandlesAcronyms(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"RabbitMQStreaming":                       "rabbit-mq-streaming",
		"RabbitMQStreaming-DeleteRabbitMQCluster": "rabbit-mq-streaming--delete-rabbit-mq-cluster",
		"SlurmIndirect-GetJob":                    "slurm-indirect--get-job",
		"UUIDV5":                                  "uuid-v5",
		"UUIDV51":                                 "uuid-v51",
		"UUIDV5AaAA":                              "uuid-v5-aa-aa",
		"HTTPServer":                              "http-server",
	}

	for input, want := range tests {
		got := slug(input)
		if got != want {
			t.Fatalf("slug(%q) = %q, want %q", input, got, want)
		}
	}
}

func newExecTestRuntime(
	t *testing.T,
	method proto.MethodInfo,
	scopes []string,
	introspectionOK bool,
) *runtime.Runtime {
	t.Helper()

	state := auth.NewState()
	rec := auth.TokenRecord{
		Token:   "test-token",
		Project: "test-project",
		Enclave: headermap.DefaultEnclave,
		Scopes:  scopes,
	}
	if !introspectionOK {
		rec.LastIntrospectionError = "introspection unavailable"
	}

	if err := state.PutToken(rec); err != nil {
		t.Fatalf("put token: %v", err)
	}

	return &runtime.Runtime{
		State:   state,
		Methods: []proto.MethodInfo{method},
	}
}
