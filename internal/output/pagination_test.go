package output

import (
	"strings"
	"testing"
)

func TestPaginationTextSummary(t *testing.T) {
	pagination := Pagination{
		Limit:      25,
		Offset:     150,
		Returned:   25,
		NextOffset: 175,
		HasMore:    true,
	}

	summary := pagination.TextSummary()

	if !strings.Contains(summary, "Showing 25 items") {
		t.Fatalf("expected item count summary, got: %s", summary)
	}
	if !strings.Contains(summary, "offset 150") || !strings.Contains(summary, "limit 25") {
		t.Fatalf("expected offset and limit details, got: %s", summary)
	}
	if !strings.Contains(summary, "More available") {
		t.Fatalf("expected more available hint, got: %s", summary)
	}
	if !strings.Contains(summary, "change page size with --limit N") {
		t.Fatalf("expected limit hint, got: %s", summary)
	}
}

func TestPaginationTextSummary_NoMore(t *testing.T) {
	pagination := Pagination{
		Limit:    25,
		Offset:   150,
		Returned: 10,
		HasMore:  false,
	}

	summary := pagination.TextSummary()

	if !strings.Contains(summary, "No more results") {
		t.Fatalf("expected no more results hint, got: %s", summary)
	}
	if strings.Contains(summary, "change page size with --limit N") {
		t.Fatalf("unexpected limit hint, got: %s", summary)
	}
}
