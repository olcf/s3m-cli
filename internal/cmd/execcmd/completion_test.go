package execcmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	commonpb "github.com/olcf/s3m-apis/common/v1alpha"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/urfave/cli/v3"

	"github.com/olcf/s3m-cli/internal/auth"
	"github.com/olcf/s3m-cli/internal/headermap"
	iproto "github.com/olcf/s3m-cli/internal/proto"
	"github.com/olcf/s3m-cli/internal/runtime"
)

func TestExecParamCompletion(t *testing.T) {
	t.Setenv("SHELL", "bash")

	method := buildMethodInfo(t)
	// Add allowed values to the header param for this test
	method.Headers[0].AllowedValues = []string{"defiant", "quokka"}

	rt := &runtime.Runtime{
		State:   auth.NewState(),
		Methods: []iproto.MethodInfo{method},
	}

	execCmd := BuildExecCommand(rt)
	cmd := &cli.Command{
		Name:                  "s3m",
		EnableShellCompletion: true,
		Commands:              []*cli.Command{execCmd},
	}

	tests := []struct {
		name     string
		args     []string
		want     []string
		wantMiss []string
	}{
		{
			name: "base params",
			args: []string{"s3m", "exec", "slurm/v0043", "get-job", "--generate-shell-completion"},
			want: []string{
				"compute_resource=defiant",
				"compute_resource=quokka",
				"compute_resource=",
				"job_id=",
				"details=",
			},
		},
		{
			name: "filter provided",
			args: []string{"s3m", "exec", "slurm/v0043", "get-job", "compute_resource=defiant", "--generate-shell-completion"},
			want: []string{
				"job_id=",
				"details=",
			},
			wantMiss: []string{
				"compute_resource=",
			},
		},
		{
			name: "trailing equals shows allowed values",
			args: []string{"s3m", "exec", "slurm/v0043", "get-job", "compute_resource=", "--generate-shell-completion"},
			want: []string{
				"compute_resource=defiant",
				"compute_resource=quokka",
				"compute_resource=",
			},
			wantMiss: []string{
				"job_id=",
				"details=",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			cmd.Writer = &buf

			if err := cmd.Run(context.Background(), tt.args); err != nil {
				t.Fatalf("run completion: %v", err)
			}

			output := buf.String()
			lines := strings.Split(strings.TrimSpace(output), "\n")

			for _, want := range tt.want {
				found := false
				for _, got := range lines {
					if got == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected completion %q not found in output:\n%s", want, output)
				}
			}

			for _, wantMiss := range tt.wantMiss {
				if strings.Contains(output, wantMiss) {
					t.Errorf("expected completion %q to be missing, but found in output:\n%s", wantMiss, output)
				}
			}
		})
	}
}

func TestExecCompletionIncludesParamsForFullSlug(t *testing.T) {
	t.Setenv("SHELL", "bash")

	method := buildMethodInfo(t)

	rt := &runtime.Runtime{
		State:   auth.NewState(),
		Methods: []iproto.MethodInfo{method},
	}

	execCmd := BuildExecCommand(rt)
	cmd := &cli.Command{
		Name:                  "s3m",
		EnableShellCompletion: true,
		Commands:              []*cli.Command{execCmd},
	}

	var buf bytes.Buffer
	cmd.Writer = &buf

	args := []string{"s3m", "exec", "slurm/v0043", "slurm-indirect--get-job", "--generate-shell-completion"}
	if err := cmd.Run(context.Background(), args); err != nil {
		t.Fatalf("run completion: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) == 0 {
		t.Fatalf("expected parameter completions, got none")
	}

	if lines[0] == "" || !strings.Contains(strings.Join(lines, " "), "compute_resource=") {
		t.Fatalf("expected header param completion, got %v", lines)
	}
	if !strings.Contains(strings.Join(lines, " "), "job_id=") {
		t.Fatalf("expected body param completion, got %v", lines)
	}
}

func buildMethodInfo(t *testing.T) iproto.MethodInfo {
	t.Helper()

	so := &descriptorpb.ServiceOptions{}
	gproto.SetExtension(so, commonpb.E_ServiceHeaderParam, []*commonpb.HeaderParam{
		{Header: "olcf-resource", ParamName: "compute_resource", Required: *gproto.Bool(true)},
	})

	reqMsg := &descriptorpb.DescriptorProto{
		Name: gproto.String("GetJobReq"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:     gproto.String("job_id"),
				Number:   gproto.Int32(1),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_REQUIRED.Enum(),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				JsonName: gproto.String("job_id"),
			},
			{
				Name:   gproto.String("details"),
				Number: gproto.Int32(2),
				Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Type:   descriptorpb.FieldDescriptorProto_TYPE_BOOL.Enum(),
			},
		},
	}

	fdp := &descriptorpb.FileDescriptorProto{
		Name:    gproto.String("slurm.proto"),
		Package: gproto.String("olcf.s3m.slurm.v0043"),
		Syntax:  gproto.String("proto2"),
		MessageType: []*descriptorpb.DescriptorProto{
			reqMsg,
			{Name: gproto.String("GetJobResp")},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name:    gproto.String("SlurmIndirect"),
			Options: so,
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name:       gproto.String("GetJob"),
				InputType:  gproto.String(".olcf.s3m.slurm.v0043.GetJobReq"),
				OutputType: gproto.String(".olcf.s3m.slurm.v0043.GetJobResp"),
			}},
		}},
	}

	files, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}})
	if err != nil {
		t.Fatalf("NewFiles: %v", err)
	}

	fd, err := files.FindFileByPath("slurm.proto")
	if err != nil {
		t.Fatalf("FindFileByPath: %v", err)
	}

	sd := fd.Services().ByName("SlurmIndirect")
	md := sd.Methods().ByName("GetJob")

	headers := iproto.HeaderParamsForMethod(headermap.DefaultEnclave, headermap.DefaultResolver(), sd, md)

	return iproto.MethodInfo{
		File:    fd,
		Service: sd,
		Method:  md,
		Headers: headers,
		ToolName: iproto.ToolNameForMethod(
			fd, sd, md,
		),
		Path:    iproto.BuildRESTPath("slurm", "v0043", md),
		Desc:    "Get job",
		API:     "slurm",
		Version: "v0043",
	}
}
