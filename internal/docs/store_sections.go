package docs

import "strings"

//
// Document section parsing

//nolint:funlen
func splitDocSections(doc *Doc) []*DocSection {
	lines := strings.Split(doc.Body, "\n")
	sections := make([]*DocSection, 0)
	buffer := make([]string, 0)

	defaultSection := &DocSection{
		Doc:          doc,
		SectionTitle: "Overview",
		Level:        0,
	}
	stack := []*DocSection{defaultSection}
	current := defaultSection

	flush := func() {
		if current == nil {
			return
		}

		current.Content = strings.TrimSpace(strings.Join(buffer, "\n"))
		current.LowerText = strings.ToLower(current.SectionTitle + "\n" + current.Content)

		sections = append(sections, current)
		buffer = buffer[:0]
	}

	for _, line := range lines {
		if lvl, title, tags, ok := parseHeadingLine(line); ok {
			flush()

			for len(stack) > 0 && stack[len(stack)-1].Level >= lvl {
				stack = stack[:len(stack)-1]
			}

			var parent *DocSection
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			}

			section := &DocSection{
				Doc:          doc,
				SectionTitle: title,
				Level:        lvl,
				Tags:         tags,
				Parent:       parent,
			}

			if parent != nil {
				parent.Children = append(parent.Children, section)
			}

			stack = append(stack, section)
			current = section

			continue
		}

		buffer = append(buffer, line)
	}

	flush()

	filtered := make([]*DocSection, 0, len(sections))

	for _, section := range sections {
		if section.Level == 0 && section.SectionTitle == "Overview" && section.Content == "" {
			continue
		}

		filtered = append(filtered, section)
	}

	return filtered
}

func parseHeadingLine(line string) (level int, title string, tags []string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "#") {
		return 0, "", nil, false
	}

	i := 0
	for i < len(trimmed) && trimmed[i] == '#' {
		i++
	}

	if i == 0 || i >= len(trimmed) || trimmed[i] != ' ' {
		return 0, "", nil, false
	}

	level = i
	rest := strings.TrimSpace(trimmed[i+1:])

	if rest == "" {
		return 0, "", nil, false
	}

	title, tags = extractHeadingTags(rest)

	return level, title, tags, true
}

func extractHeadingTags(text string) (string, []string) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasSuffix(trimmed, ")") {
		return trimmed, nil
	}

	open := strings.LastIndex(trimmed, "(")
	if open == -1 {
		return trimmed, nil
	}

	tagSegment := trimmed[open+1 : len(trimmed)-1]
	parts := strings.Split(tagSegment, ",")
	tags := make([]string, 0, len(parts))

	for _, part := range parts {
		tag := strings.ToLower(strings.TrimSpace(part))
		if tag == "" || !tagPattern.MatchString(tag) {
			return trimmed, nil
		}

		tags = append(tags, tag)
	}

	if len(tags) == 0 {
		return trimmed, nil
	}

	return strings.TrimSpace(trimmed[:open]), tags
}
