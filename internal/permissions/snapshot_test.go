package permissions

import (
	"testing"

	slurmv0042pb "github.com/olcf/s3m-apis/slurm/v0042"

	"github.com/olcf/s3m-cli/internal/proto"
)

func TestSnapshotMatching(t *testing.T) {
	file := slurmv0042pb.File_proto_slurm_v0042_slurm_proto
	service := file.Services().ByName("SlurmIndirect")
	md := service.Methods().ByName("GetJobs")
	method := proto.MethodInfo{
		Service: service,
		Method:  md,
	}

	exact := New([]string{"/olcf.s3m.slurm.v0042.SlurmIndirect/GetJobs"}, true)
	if !exact.Can(method) {
		t.Fatal("exact gRPC path should allow method")
	}

	deny := New([]string{"/other.service/OtherMethod"}, true)
	if deny.Can(method) {
		t.Fatal("unrelated scope should deny method")
	}

	unknown := New(nil, false)
	if unknown.Can(method) {
		t.Fatal("unknown permissions should deny execution")
	}

	wildcard := New([]string{"/olcf.s3m.slurm.v0042.SlurmIndirect/*"}, true)
	if !wildcard.Can(method) {
		t.Fatal("service wildcard should allow method")
	}

	star := New([]string{"*"}, true)
	if !star.Can(method) {
		t.Fatal("star scope should allow any method")
	}
}

func TestSnapshotKnownIsFalseWithoutUsablePaths(t *testing.T) {
	tests := []struct {
		name   string
		scopes []string
	}{
		{name: "empty scopes", scopes: nil},
		{name: "coarse only", scopes: []string{"compute-ace", "slurm/v0042"}},
		{name: "malformed path-like", scopes: []string{"/only-service", "/service/method/extra"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if snap := New(tt.scopes, true); snap.Known {
				t.Fatalf("expected Known=false for scopes %v", tt.scopes)
			}
		})
	}
}

func TestSnapshotMixedCoarseAndPathScopesIsKnown(t *testing.T) {
	file := slurmv0042pb.File_proto_slurm_v0042_slurm_proto
	service := file.Services().ByName("SlurmIndirect")
	md := service.Methods().ByName("GetJobs")
	method := proto.MethodInfo{
		Service: service,
		Method:  md,
	}

	snap := New([]string{"compute-ace", "/olcf.s3m.slurm.v0042.SlurmIndirect/GetJobs"}, true)
	if !snap.Known {
		t.Fatalf("expected Known=true when at least one usable gRPC path is present")
	}
	if !snap.Can(method) {
		t.Fatal("expected usable gRPC path to allow matching method")
	}
}
