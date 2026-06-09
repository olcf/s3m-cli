package auth

import (
	"testing"

	tmspb "github.com/olcf/s3m-apis/tms/v1"
)

func TestCollectScopesNormalizesGrpcPermissions(t *testing.T) {
	td := &tmspb.TokenDetailsIntrospective{
		Project:         "proj",
		SecurityEnclave: "enc",
		Permissions:     []string{"compute-ace"},
		GrpcPermissions: []*tmspb.GRPCPermissions{
			{
				Path:       "/olcf.s3m.slurm.v0042.SlurmIndirect/GetJobs",
				Permission: "Slurm/V0042",
			},
		},
	}

	scopes := collectScopes(td)

	found := make(map[string]bool)
	for _, s := range scopes {
		found[s] = true
	}

	if !found["compute-ace"] {
		t.Fatalf("expected base permission scope to be present")
	}

	if !found["/olcf.s3m.slurm.v0042.slurmindirect/getjobs"] {
		t.Fatalf("expected normalized gRPC permission path to be present")
	}

	if found["/olcf.s3m.slurm.v0042.SlurmIndirect/GetJobs"] {
		t.Fatalf("did not expect mixed-case gRPC permission path: %v", scopes)
	}

	if !found["slurm/v0042"] {
		t.Fatalf("expected normalized gRPC permission name to be present")
	}

	if found["Slurm/V0042"] {
		t.Fatalf("did not expect mixed-case gRPC permission name: %v", scopes)
	}
}
