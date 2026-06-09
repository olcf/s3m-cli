package headermap

import (
	"reflect"
	"testing"
)

func TestDefaultResolverProvidesAllowedValues(t *testing.T) {
	resolver := DefaultResolver()

	want := []string{"defiant", "quokka", "wombat"}

	got := resolver.ServiceValues("open", "olcf.s3m.slurm.v0042.SlurmIndirect", "olcf-resource")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected values: %v", got)
	}

	// Unknown enclave should fall back to the default enclave mapping.
	got = resolver.ServiceValues("unknown", "olcf.s3m.slurm.v0042.SlurmIndirect", "olcf-resource")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected fallback values: %v", got)
	}

	if vals := resolver.MethodValues(DefaultEnclave, "olcf.s3m.slurm.v0042.SlurmIndirect.GetJobs", "olcf-resource"); vals != nil {
		t.Fatalf("expected no method-level defaults, got %v", vals)
	}
}
