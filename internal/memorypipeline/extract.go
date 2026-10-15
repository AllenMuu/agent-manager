package memorypipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/run"
)

const (
	maxEvidenceFieldBytes = 8 * 1024
	maxEvidenceItems      = 64
	maxEvidenceEvents     = 256
	maxNormalizedEvidence = 2 << 20
)

type EvidenceField string

const (
	FieldDecision       EvidenceField = "decision"
	FieldDiscovery      EvidenceField = "discovery"
	FieldConstraint     EvidenceField = "constraint"
	FieldKnownIssue     EvidenceField = "known_issue"
	FieldCommand        EvidenceField = "command"
	FieldFailedApproach EvidenceField = "failed_approach"
)

type DerivedState string

const (
	DerivedActive     DerivedState = "active"
	DerivedSuperseded DerivedState = "superseded"
	DerivedExpired    DerivedState = "expired"
)

type VerificationState string

const (
	VerificationUnverified VerificationState = "unverified"
)

// DerivedMemory is always untrusted until a separate operator-reviewed
// proposal is accepted into canonical Project Memory.
type DerivedMemory struct {
	ID           string               `json:"id"`
	Supersedes   []string             `json:"supersedes,omitempty"`
	ProjectID    string               `json:"projectId"`
	Field        EvidenceField        `json:"field"`
	Type         memory.KnowledgeType `json:"type"`
	Content      string               `json:"content"`
	EvidenceIDs  []string             `json:"evidenceIds"`
	Confidence   float64              `json:"confidence"`
	State        DerivedState         `json:"state"`
	Verification VerificationState    `json:"verification"`
	CreatedAt    time.Time            `json:"createdAt"`
	UpdatedAt    time.Time            `json:"updatedAt"`
	LastUsedAt   *time.Time           `json:"lastUsedAt,omitempty"`
	UsageCount   uint64               `json:"usageCount"`
}

type Extraction struct {
	ProjectID          string          `json:"projectId"`
	EvidenceID         string          `json:"evidenceId"`
	Summary            string          `json:"summary"`
	SummaryEvidenceIDs []string        `json:"summaryEvidenceIds"`
	Memories           []DerivedMemory `json:"memories"`
}

type extractedFact struct {
	field   EvidenceField
	kind    memory.KnowledgeType
	content string
}

// ExtractEvidence deterministically emits only source-present information.
// It uses no model, network, clock read, or filesystem write.
func ExtractEvidence(evidence RunEvidence) (Extraction, error) {
	ref, err := referenceFor(evidence)
	if err != nil {
		return Extraction{}, err
	}
	if evidence.Ref.ID == "" || evidence.Ref.ContentHash == "" || !sameReference(ref, evidence.Ref) {
		return Extraction{}, ErrEvidenceChanged
	}

	summary := strings.TrimSpace(evidence.SummaryText)
	if summary == "" {
		summary = deterministicSummary(evidence)
	}
	if len(summary) > maxEvidenceFieldBytes {
		return Extraction{}, fmt.Errorf("%w: summary exceeds %d bytes", ErrInvalidEvidence, maxEvidenceFieldBytes)
	}

	facts := make([]extractedFact, 0, len(evidence.Events)+maxEvidenceItems)
	for _, value := range evidence.Decisions {
		facts = append(facts, extractedFact{FieldDecision, memory.TypeDecision, value})
	}
	for _, value := range evidence.Discoveries {
		facts = append(facts, extractedFact{FieldDiscovery, memory.TypeFact, value})
	}
	for _, value := range evidence.Constraints {
		facts = append(facts, extractedFact{FieldConstraint, memory.TypeConstraint, value})
	}
	for _, value := range evidence.KnownIssues {
		facts = append(facts, extractedFact{FieldKnownIssue, memory.TypeExperience, value})
	}
	for _, value := range evidence.Commands {
		facts = append(facts, extractedFact{FieldCommand, memory.TypeExperience, value})
	}
	for _, value := range evidence.FailedApproaches {
		facts = append(facts, extractedFact{FieldFailedApproach, memory.TypeExperience, value})
	}
	for _, event := range evidence.Events {
		if event.Decision != "" {
			content := fmt.Sprintf("Policy decision %s recorded for %s", event.Decision, event.Category)
			if event.Reason != "" {
				content += "; reason " + event.Reason
			}
			facts = append(facts, extractedFact{FieldDecision, memory.TypeDecision, content + "."})
		}
	}
	for _, control := range evidence.PolicyControls {
		if control == "" {
			continue
		}
		content := "Run evidence records the configured governance control " + control + "."
		if evidence.PolicyID != "" && evidence.PolicyVersion != "" {
			content = fmt.Sprintf("Policy %s@%s records the governance control %s.", evidence.PolicyID, evidence.PolicyVersion, control)
		}
		facts = append(facts, extractedFact{FieldConstraint, memory.TypeConstraint, content})
	}
	if reasonIsIssue(evidence.Termination) {
		facts = append(facts, extractedFact{FieldKnownIssue, memory.TypeExperience, "Run termination was recorded with reason " + evidence.Termination + "."})
	}
	for _, event := range evidence.Events {
		if event.Result == string(run.InvocationFailed) || event.Result == string(run.InvocationBlocked) {
			facts = append(facts, extractedFact{FieldKnownIssue, memory.TypeExperience, fmt.Sprintf("A runtime invocation was recorded as %s for %s.", event.Result, event.Category)})
		}
	}

	unique := make(map[string]extractedFact, len(facts))
	for _, fact := range facts {
		fact.content = strings.TrimSpace(fact.content)
		if fact.content == "" {
			continue
		}
		if len(fact.content) > maxEvidenceFieldBytes {
			return Extraction{}, fmt.Errorf("%w: extracted %s exceeds %d bytes", ErrInvalidEvidence, fact.field, maxEvidenceFieldBytes)
		}
		key := string(fact.field) + "\x00" + fact.content
		unique[key] = fact
	}
	facts = facts[:0]
	for _, fact := range unique {
		facts = append(facts, fact)
	}
	sort.Slice(facts, func(i, j int) bool {
		if facts[i].field == facts[j].field {
			return facts[i].content < facts[j].content
		}
		return facts[i].field < facts[j].field
	})

	createdAt := evidence.Ref.CreatedAt.UTC()
	memories := make([]DerivedMemory, 0, len(facts))
	for _, fact := range facts {
		memories = append(memories, DerivedMemory{
			ID: derivedID(ref.ProjectID, ref.ID, fact.field, fact.content), ProjectID: ref.ProjectID,
			Field: fact.field, Type: fact.kind, Content: fact.content, EvidenceIDs: []string{ref.ID},
			Confidence: 1, State: DerivedActive, Verification: VerificationUnverified,
			CreatedAt: createdAt, UpdatedAt: createdAt,
		})
	}
	return Extraction{ProjectID: ref.ProjectID, EvidenceID: ref.ID, Summary: summary, SummaryEvidenceIDs: []string{ref.ID}, Memories: memories}, nil
}

func deterministicSummary(evidence RunEvidence) string {
	ended := evidence.Status
	if ended == "" {
		ended = "terminal"
	}
	summary := fmt.Sprintf("Run %s (%s) ended as %s", evidence.Ref.NativeRunID, evidence.Ref.Runtime, ended)
	if evidence.PolicyID != "" && evidence.PolicyVersion != "" {
		summary += fmt.Sprintf(" under policy %s@%s", evidence.PolicyID, evidence.PolicyVersion)
	}
	if len(evidence.Events) > 0 {
		summary += fmt.Sprintf("; %d governance events recorded", len(evidence.Events))
	}
	return summary + "."
}

func sameReference(expected, actual RunEvidenceRef) bool {
	return expected.ID == actual.ID && expected.ProjectID == actual.ProjectID && expected.Runtime == actual.Runtime &&
		expected.NativeRunID == actual.NativeRunID && expected.SourceRef == actual.SourceRef &&
		expected.CreatedAt.Equal(actual.CreatedAt) && expected.ContentHash == actual.ContentHash && expected.Status == actual.Status
}

func derivedID(projectID, evidenceID string, field EvidenceField, content string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{projectID, evidenceID, string(field), content}, "\x00")))
	return "dm_" + hex.EncodeToString(sum[:16])
}

func reasonIsIssue(reason string) bool {
	switch reason {
	case run.TerminationPolicyViolation, run.TerminationBudgetExhausted, run.TerminationUnexpected, run.TerminationRuntimeFailure:
		return true
	default:
		return false
	}
}
