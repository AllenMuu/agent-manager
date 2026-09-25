package recommend

import (
	"fmt"

	"github.com/AllenMuu/skill-manager/internal/catalog"
	"github.com/AllenMuu/skill-manager/internal/stack"
)

// Status is the outcome classification of one evaluated scope.
type Status string

const (
	StatusRecommended          Status = "recommended"
	StatusInsufficientEvidence Status = "insufficient_evidence"
	StatusNoCatalogMatch       Status = "no_catalog_match"
)

// Action is the documented next step for one evaluated scope.
type Action string

const (
	ActionReviewRecommendations Action = "review_recommendations"
	ActionUseCatalogSearch      Action = "use_catalog_search"
	ActionAddSkillMetadata      Action = "add_skill_metadata"
)

// ScopeResult is one evaluated scope of a recommendation result.
type ScopeResult struct {
	Path            string
	Status          Status
	Technologies    []stack.Technology
	Evidence        []stack.Evidence
	Diagnostics     []stack.Diagnostic
	Recommendations []Recommendation
	NextAction      Action
}

// Result is a complete, read-only recommendation outcome.
type Result struct {
	Project      string
	ScanComplete bool
	Diagnostics  []stack.Diagnostic
	Scopes       []ScopeResult
}

// Recommend scans project statically and ranks matching eligible skills from
// the configured library. It performs no mutation of any kind.
func Recommend(project, libraryPath string) (Result, error) {
	skills, _, err := catalog.Discover(libraryPath)
	if err != nil {
		return Result{}, fmt.Errorf("read skill library %s: %w", libraryPath, err)
	}
	scan, err := stack.Scan(project)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		Project:      scan.Project,
		ScanComplete: scan.ScanComplete,
		Diagnostics:  scan.Diagnostics,
	}
	for _, scope := range scan.Scopes {
		result.Scopes = append(result.Scopes, evaluateScope(scope, skills))
	}
	return result, nil
}

func evaluateScope(scope stack.Scope, skills []catalog.Skill) ScopeResult {
	out := ScopeResult{Path: scope.Path}
	out.Evidence = scope.Evidence
	out.Diagnostics = scope.Diagnostics
	for _, id := range scope.Technologies {
		if tech, ok := stack.Lookup(id); ok {
			out.Technologies = append(out.Technologies, tech)
		}
	}
	if len(out.Technologies) == 0 {
		out.Status = StatusInsufficientEvidence
		out.NextAction = ActionUseCatalogSearch
		return out
	}
	out.Recommendations = MatchScope(skills, scope.Technologies)
	if len(out.Recommendations) > 0 {
		out.Status = StatusRecommended
		out.NextAction = ActionReviewRecommendations
		return out
	}
	out.Status = StatusNoCatalogMatch
	out.NextAction = ActionAddSkillMetadata
	return out
}
