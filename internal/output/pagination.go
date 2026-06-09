package output

import (
	"fmt"
	"strings"
)

// Pagination describes list pagination state for rendering.
type Pagination struct {
	Limit         uint32 `json:"limit"`
	Offset        uint32 `json:"offset"`
	Returned      uint32 `json:"returned"`
	NextOffset    uint32 `json:"nextOffset"`
	HasMore       bool   `json:"hasMore"`
	NextPageToken string `json:"nextPageToken"`
}

// TextSummary renders a human-friendly pagination line for text output.
func (p Pagination) TextSummary() string {
	base := fmt.Sprintf("Showing %d items", p.Returned)

	details := make([]string, 0, 2)
	if p.Offset > 0 {
		details = append(details, fmt.Sprintf("offset %d", p.Offset))
	}

	if p.Limit > 0 {
		details = append(details, fmt.Sprintf("limit %d", p.Limit))
	}

	if len(details) > 0 {
		base = base + " (" + strings.Join(details, ", ") + ")"
	}

	actions := make([]string, 0, 2)
	if p.HasMore {
		actions = append(actions, fmt.Sprintf("More available, use --offset %d", p.NextOffset))
		actions = append(actions, "change page size with --limit N")
	} else {
		actions = append(actions, "No more results")
	}

	actionsText := strings.Join(actions, "; ")
	if actionsText != "" {
		actionsText = strings.ToUpper(actionsText[:1]) + actionsText[1:]
	}

	return fmt.Sprintf("%s. %s.", base, actionsText)
}
