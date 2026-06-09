package docs

import (
	"regexp"

	"github.com/olcf/s3m-cli/internal/proto"
)

const maxSectionsPerDoc = 5

var (
	tagPattern  = regexp.MustCompile(`^[a-z0-9-]+$`)
	punctuation = regexp.MustCompile(`[^\w\s-]`)
)

//
// Types

type docSelector struct {
	Tool     string   `json:"tool"`
	API      string   `json:"api"`
	Service  string   `json:"service"`
	Versions []string `json:"versions"`
	Methods  []string `json:"methods"`
}

type docFrontMatter struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Tags      []string      `json:"tags"`
	Selectors []docSelector `json:"selectors"`
}

// ToolAliases maps an exposed tool name to equivalent tool names used by docs.
type ToolAliases map[string][]string

type Doc struct {
	ID         string
	Title      string
	Tags       []string
	Body       string
	Path       string
	AppliesTo  []string
	Sections   []*DocSection
	toolLookup map[string]struct{}
}

type DocSection struct {
	Doc          *Doc
	SectionTitle string
	Content      string
	LowerText    string
	Tags         []string
	Level        int
	Parent       *DocSection
	Children     []*DocSection
}

type Store struct {
	Docs       map[string]*Doc
	toolDocs   map[string][]*Doc
	tagCounts  map[string]int
	sections   []*DocSection
	methodMeta []methodTarget
	aliases    toolAliasIndex
}

type methodTarget struct {
	ToolName    string
	API         string
	ServiceName string
	MethodName  string
	Version     string
}

type SectionHit struct {
	Section *DocSection
	Score   int
}

type DocMatch struct {
	Doc      *Doc
	Sections []SectionHit
	Score    int
}

type TagSummary struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

type toolAliasIndex struct {
	equivalents map[string]map[string]struct{}
	display     map[string]string
}

//
// Constructors

func newStore(methods []proto.MethodInfo, aliases ...ToolAliases) *Store {
	methodTargets := buildMethodTargets(methods)

	return &Store{
		Docs:       make(map[string]*Doc),
		toolDocs:   make(map[string][]*Doc),
		tagCounts:  make(map[string]int),
		sections:   make([]*DocSection, 0),
		methodMeta: methodTargets,
		aliases:    newToolAliasIndex(methodTargets, mergeToolAliases(aliases...)),
	}
}
