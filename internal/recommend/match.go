// Package recommend ranks catalog skills against detected project stacks.
package recommend

import (
	"sort"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/catalog"
	"github.com/AllenMuu/skill-manager/internal/stack"
)

// field weights for one complete matched technology term.
const (
	tagWeight         = 3
	identifierWeight  = 2
	descriptionWeight = 2
	bodyWeight        = 1
)

// Confidence bands derived from the strongest matched field.
type Confidence string

const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
	ConfidenceLow    Confidence = "low"
)

// Recommendation is one ranked skill with its explainable evidence.
type Recommendation struct {
	Identifier       string
	Description      string
	MatchReason      string
	MatchingEvidence []Match
	Confidence       Confidence
	Score            int
}

// Match records one technology hit in one catalog field.
type Match struct {
	Technology string `json:"technology"`
	Field      string `json:"field"`
}

// textAliases lists complete-term spellings recognized in free text for
// vocabulary identifiers whose display form differs from their canonical id.
var textAliases = map[string][]string{
	"nodejs":      {"nodejs", "node js"},
	"nextjs":      {"nextjs", "next js"},
	"postgresql":  {"postgresql", "postgres"},
	"mongodb":     {"mongodb", "mongo db"},
	"spring-boot": {"spring boot"},
	"claude-code": {"claude code", "claude code cli"},
}

// MatchScope ranks every eligible catalog skill against one scope's
// detected technologies.
func MatchScope(skills []catalog.Skill, technologies []string) []Recommendation {
	var recommendations []Recommendation
	for _, skill := range skills {
		rec := matchSkill(skill, technologies)
		if rec.Score > 0 {
			recommendations = append(recommendations, rec)
		}
	}
	sort.Slice(recommendations, func(i, j int) bool {
		if recommendations[i].Score != recommendations[j].Score {
			return recommendations[i].Score > recommendations[j].Score
		}
		return recommendations[i].Identifier < recommendations[j].Identifier
	})
	return recommendations
}

func matchSkill(skill catalog.Skill, technologies []string) Recommendation {
	rec := Recommendation{Identifier: skill.Identifier, Description: skill.Description}
	tagSet := tagSet(skill.Tags)
	for _, tech := range technologies {
		if tagSet[tech] {
			rec.Score += tagWeight
			rec.MatchingEvidence = append(rec.MatchingEvidence, Match{Technology: tech, Field: "tags"})
		}
		if termMatches(skill.Identifier, tech) {
			rec.Score += identifierWeight
			rec.MatchingEvidence = append(rec.MatchingEvidence, Match{Technology: tech, Field: "identifier"})
		}
		if termMatches(skill.Description, tech) {
			rec.Score += descriptionWeight
			rec.MatchingEvidence = append(rec.MatchingEvidence, Match{Technology: tech, Field: "description"})
		}
		if termMatches(skill.Body, tech) {
			rec.Score += bodyWeight
			rec.MatchingEvidence = append(rec.MatchingEvidence, Match{Technology: tech, Field: "body"})
		}
	}
	if rec.Score == 0 {
		return rec
	}
	rec.Confidence = confidenceFor(rec.MatchingEvidence)
	rec.MatchReason = reasonFor(rec.MatchingEvidence)
	return rec
}

func confidenceFor(matches []Match) Confidence {
	best := ConfidenceLow
	for _, m := range matches {
		switch m.Field {
		case "tags":
			return ConfidenceHigh
		case "identifier", "description":
			best = ConfidenceMedium
		}
	}
	return best
}

func reasonFor(matches []Match) string {
	var parts []string
	for _, m := range matches {
		parts = append(parts, m.Technology+" via "+m.Field)
	}
	sort.Strings(parts)
	return "matched " + strings.Join(parts, ", ")
}

// tagSet returns the exact lowercase companion-tag set. Only tags that
// exactly equal a vocabulary identifier count as technology tags.
func tagSet(tags []string) map[string]bool {
	set := map[string]bool{}
	for _, t := range tags {
		set[stack.NormalizeToken(t)] = true
	}
	return set
}

// termMatches reports whether text contains the technology as one or more
// complete words, never as an arbitrary substring.
func termMatches(text, technology string) bool {
	if text == "" {
		return false
	}
	terms := append([]string{technology}, textAliases[technology]...)
	for _, term := range terms {
		if wordBoundaryContains(text, term) {
			return true
		}
	}
	return false
}

// wordBoundaryContains reports whether term appears in text surrounded by
// non-alphanumeric boundaries.
func wordBoundaryContains(text, term string) bool {
	if term == "" {
		return false
	}
	return strings.Contains(" "+normalizeWords(text)+" ", " "+normalizeWords(term)+" ")
}

// normalizeWords lowercases text and replaces every non-alphanumeric rune
// with a space so punctuation never glues words together.
func normalizeWords(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		b.WriteRune(' ')
	}
	return b.String()
}
