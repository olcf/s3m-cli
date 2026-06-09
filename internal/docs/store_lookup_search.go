package docs

import (
	"sort"
	"strings"
)

//
// Lookup

func (ds *Store) LookupByTool(tool string) []*Doc {
	if tool == "" {
		return nil
	}

	tools := ds.aliases.expand(tool)
	if len(tools) == 0 {
		return nil
	}

	seen := make(map[*Doc]struct{})
	docs := make([]*Doc, 0)

	for _, name := range tools {
		for _, doc := range ds.toolDocs[strings.ToLower(name)] {
			if _, ok := seen[doc]; ok {
				continue
			}

			seen[doc] = struct{}{}
			docs = append(docs, doc)
		}
	}

	sort.Slice(docs, func(i, j int) bool {
		return docs[i].ID < docs[j].ID
	})

	return docs
}

func (ds *Store) LookupByID(id string) (*Doc, bool) {
	doc, ok := ds.Docs[id]
	return doc, ok
}

func (ds *Store) HasDocs(tool string) bool {
	return len(ds.LookupByTool(tool)) > 0
}

func (d *Doc) AppliesToTool(tool string) bool {
	if d == nil || tool == "" {
		return false
	}

	_, ok := d.toolLookup[strings.ToLower(tool)]

	return ok
}

//
// Search

//nolint:cyclop,funlen
func (ds *Store) SearchDocs(query string, tags []string, limit, offset int) ([]DocMatch, bool) {
	tokens := tokenizeQuery(query)
	if len(tokens) == 0 {
		tokens = nil
	}

	normalizedTags := normalizeTags(tags)

	if len(tokens) == 0 && len(normalizedTags) == 0 {
		return nil, false
	}

	type docAggregate struct {
		sections  []SectionHit
		bestScore int
	}

	aggregates := make(map[*Doc]*docAggregate)

	for _, section := range ds.sections {
		if len(normalizedTags) > 0 && !sectionMatchesTags(section, normalizedTags) {
			continue
		}

		score := 1
		if len(tokens) > 0 {
			score = scoreSection(section, tokens)
			if score == 0 {
				continue
			}
		}

		agg := aggregates[section.Doc]
		if agg == nil {
			agg = &docAggregate{}
			aggregates[section.Doc] = agg
		}

		agg.sections = append(agg.sections, SectionHit{Section: section, Score: score})

		if score > agg.bestScore {
			agg.bestScore = score
		}
	}

	if len(aggregates) == 0 {
		return []DocMatch{}, false
	}

	if limit <= 0 {
		limit = 3
	}

	matches := make([]DocMatch, 0, len(aggregates))

	for doc, agg := range aggregates {
		sort.Slice(agg.sections, func(i, j int) bool {
			if agg.sections[i].Score == agg.sections[j].Score {
				return agg.sections[i].Section.SectionTitle < agg.sections[j].Section.SectionTitle
			}

			return agg.sections[i].Score > agg.sections[j].Score
		})

		if len(agg.sections) > maxSectionsPerDoc {
			agg.sections = agg.sections[:maxSectionsPerDoc]
		}

		matches = append(matches, DocMatch{
			Doc:      doc,
			Sections: agg.sections,
			Score:    agg.bestScore,
		})
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score == matches[j].Score {
			return matches[i].Doc.Title < matches[j].Doc.Title
		}

		return matches[i].Score > matches[j].Score
	})

	if offset >= len(matches) {
		return []DocMatch{}, false
	}

	end := min(offset+limit, len(matches))
	more := end < len(matches)

	return matches[offset:end], more
}

func tokenizeQuery(query string) []string {
	clean := punctuation.ReplaceAllString(query, " ")
	fields := strings.Fields(strings.ToLower(clean))
	uniq := make(map[string]struct{}, len(fields))
	tokens := make([]string, 0, len(fields))

	for _, f := range fields {
		if len(f) == 0 {
			continue
		}

		if _, seen := uniq[f]; seen {
			continue
		}

		uniq[f] = struct{}{}

		tokens = append(tokens, f)
	}

	return tokens
}

func normalizeTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := make(map[string]struct{})

	for _, tag := range tags {
		t := strings.ToLower(tag)
		if _, ok := seen[t]; ok {
			continue
		}

		seen[t] = struct{}{}

		out = append(out, t)
	}

	return out
}

func sectionMatchesTags(section *DocSection, tags []string) bool {
	if len(tags) == 0 {
		return true
	}

	if section == nil || section.Doc == nil {
		return false
	}

	current := section
	for current != nil {
		if tagsContainAll(current.Tags, tags) {
			return true
		}

		current = current.Parent
	}

	return tagsContainAll(section.Doc.Tags, tags)
}

func scoreSection(section *DocSection, tokens []string) int {
	score := 0

	for _, token := range tokens {
		score += strings.Count(section.LowerText, token)
	}

	return score
}

//
// Tags

func (ds *Store) TagSummaries(prefix string) []TagSummary {
	summaries := make([]TagSummary, 0, len(ds.tagCounts))

	for tag, count := range ds.tagCounts {
		if prefix != "" && !strings.HasPrefix(tag, strings.ToLower(prefix)) {
			continue
		}

		summaries = append(summaries, TagSummary{
			Tag:   tag,
			Count: count,
		})
	}

	sortTagSummaries(summaries)

	return summaries
}

// TagSummariesForDocs returns tag summaries computed from the supplied docs.
func TagSummariesForDocs(prefix string, docs []*Doc) []TagSummary {
	tagCounts := make(map[string]int)

	for _, doc := range docs {
		if doc == nil {
			continue
		}

		tagSeen := make(map[string]struct{})
		for _, tag := range doc.Tags {
			tagSeen[tag] = struct{}{}
		}

		for _, section := range doc.Sections {
			for _, tag := range section.Tags {
				tagSeen[tag] = struct{}{}
			}
		}

		for tag := range tagSeen {
			tagCounts[tag]++
		}
	}

	summaries := make([]TagSummary, 0, len(tagCounts))
	normalizedPrefix := strings.ToLower(strings.TrimSpace(prefix))

	for tag, count := range tagCounts {
		if normalizedPrefix != "" && !strings.HasPrefix(tag, normalizedPrefix) {
			continue
		}

		summaries = append(summaries, TagSummary{
			Tag:   tag,
			Count: count,
		})
	}

	sortTagSummaries(summaries)

	return summaries
}

//
// Utilities

func tagsContainAll(source []string, tags []string) bool {
	if len(source) == 0 {
		return false
	}

	set := make(map[string]struct{}, len(source))
	for _, tag := range source {
		set[strings.ToLower(tag)] = struct{}{}
	}

	for _, tag := range tags {
		if _, ok := set[tag]; !ok {
			return false
		}
	}

	return true
}

func sortTagSummaries(summaries []TagSummary) {
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Count == summaries[j].Count {
			return summaries[i].Tag < summaries[j].Tag
		}

		return summaries[i].Count > summaries[j].Count
	})
}
