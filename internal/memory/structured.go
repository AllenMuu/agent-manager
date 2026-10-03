package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

var ErrInvalidInput = errors.New("invalid Memory input")

type OwnerKind string

const (
	OwnerUser    OwnerKind = "USER"
	OwnerProject OwnerKind = "PROJECT"
	OwnerAgent   OwnerKind = "AGENT"
	OwnerSession OwnerKind = "SESSION"
)

// Owner identifies an exact partition, not the authority of a caller. AGENT
// and SESSION refine exactly one user or project; unused identifiers are rejected.
type Owner struct {
	Kind      OwnerKind `json:"kind"`
	UserID    string    `json:"userId,omitempty"`
	ProjectID string    `json:"projectId,omitempty"`
	AgentID   string    `json:"agentId,omitempty"`
	SessionID string    `json:"sessionId,omitempty"`
}

func (o Owner) validate() error {
	user, project := o.UserID != "", o.ProjectID != ""
	valid := false
	switch o.Kind {
	case OwnerUser:
		valid = user && !project && o.AgentID == "" && o.SessionID == ""
	case OwnerProject:
		valid = project && !user && o.AgentID == "" && o.SessionID == ""
	case OwnerAgent:
		valid = user != project && o.AgentID != "" && o.SessionID == ""
	case OwnerSession:
		valid = user != project && o.SessionID != "" && o.AgentID == ""
	}
	if !valid {
		return fmt.Errorf("%w: incomplete or ambiguous owner", ErrInvalidInput)
	}
	for _, id := range []string{o.UserID, o.ProjectID, o.AgentID, o.SessionID} {
		if id != "" {
			if err := validateStructuredToken("owner identifier", id); err != nil {
				return fmt.Errorf("%w: %v", ErrInvalidInput, err)
			}
		}
	}
	return nil
}

// KnowledgeType describes inert knowledge, including SKILL and TASK content.
type KnowledgeType string

const (
	TypeFact           KnowledgeType = "FACT"
	TypeConstraint     KnowledgeType = "CONSTRAINT"
	TypeDecision       KnowledgeType = "DECISION"
	TypePreference     KnowledgeType = "PREFERENCE"
	TypeExperience     KnowledgeType = "EXPERIENCE"
	TypeProcedure      KnowledgeType = "PROCEDURE"
	TypeSkill          KnowledgeType = "SKILL"
	TypeTask           KnowledgeType = "TASK"
	TypeErrorSolution  KnowledgeType = "ERROR_SOLUTION"
	TypeProjectContext KnowledgeType = "PROJECT_CONTEXT"
)

type Layer string

const (
	LayerRaw       Layer = "RAW"
	LayerAtomic    Layer = "ATOMIC"
	LayerComposite Layer = "COMPOSITE"
	LayerProfile   Layer = "PROFILE"
)

type RecordState string

const (
	RecordActive     RecordState = "ACTIVE"
	RecordSuperseded RecordState = "SUPERSEDED"
	RecordDeleted    RecordState = "DELETED"
)

type RecordID string

// NewRecord contains caller-owned metadata. ID/version/state are set on creation.
type NewRecord struct {
	Owner    Owner         `json:"owner"`
	Type     KnowledgeType `json:"type"`
	Content  string        `json:"content"`
	Source   string        `json:"source,omitempty"`
	Evidence []string      `json:"evidence,omitempty"`
	Layer    Layer         `json:"layer,omitempty"`
}

// Record is the canonical shape. Provider object IDs never replace this ID.
type Record struct {
	Supersedes   RecordID      `json:"supersedes,omitempty"`
	SupersededBy RecordID      `json:"supersededBy,omitempty"`
	ID           RecordID      `json:"id"`
	Version      uint64        `json:"version"`
	Owner        Owner         `json:"owner"`
	Type         KnowledgeType `json:"type"`
	Content      string        `json:"content"`
	Source       string        `json:"source,omitempty"`
	Evidence     []string      `json:"evidence,omitempty"`
	State        RecordState   `json:"state"`
	Layer        Layer         `json:"layer"`
}
type Query struct {
	Owner Owner  `json:"owner"`
	Text  string `json:"text,omitempty"`
}

var ErrCanceled = errors.New("Memory operation canceled")

var ErrNotFound = errors.New("Memory record not found")

func operationContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("%w: context is required", ErrInvalidInput)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %w", ErrCanceled, err)
	}
	return nil
}

// Structured errors are stable errors.Is categories. Backends wrap causes; an
// uncertain write must not be retried blindly. Unavailable shares the legacy
// provider category without changing legacy APIs.
var (
	ErrUnsupported     = errors.New("Memory operation unsupported")
	ErrUnavailable     = ErrProviderUnavailable
	ErrOwnershipDenied = errors.New("Memory ownership denied")
	ErrConflict        = errors.New("Memory version conflict")
	ErrNotCommitted    = errors.New("Memory write not committed")
	ErrOutcomeUnknown  = errors.New("Memory write outcome unknown")
)

// StructuredCapabilities describes actual implemented semantics, independent
// of configured requests and current health. Future mutations are optional.
// Update/Supersede describe operation support; ConditionalUpdate and
// AtomicSupersede separately advertise the stronger concurrency guarantees.
type StructuredCapabilities struct {
	Remember          bool `json:"remember"`
	Get               bool `json:"get"`
	Recall            bool `json:"recall"`
	ScoredRecall      bool `json:"scoredRecall"`
	Update            bool `json:"update"`
	Forget            bool `json:"forget"`
	Supersede         bool `json:"supersede"`
	History           bool `json:"history"`
	ConditionalUpdate bool `json:"conditionalUpdate"`
	AtomicSupersede   bool `json:"atomicSupersede"`
	ImportLegacy      bool `json:"importLegacy"`
}
type HealthStatus struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// StructuredProvider is separate from the legacy Provider and Searcher. Read,
// recall and write are optional contracts, so discovery cannot claim an empty
// successful result when a provider has no implementation.
type StructuredProvider interface {
	Capabilities() StructuredCapabilities
	Health(context.Context) (HealthStatus, error)
}
type RecordWriter interface {
	Remember(context.Context, NewRecord) (Record, error)
}
type RecordReader interface {
	Get(context.Context, Owner, RecordID) (Record, error)
}
type RecordRecaller interface {
	Recall(context.Context, Query) ([]Record, error)
}

func providerReady(ctx context.Context, p StructuredProvider, supported bool) error {
	if err := operationContext(ctx); err != nil {
		return err
	}
	if p == nil {
		return ErrUnavailable
	}
	if !supported {
		return ErrUnsupported
	}
	health, err := p.Health(ctx)
	if err != nil {
		return err
	}
	if !health.Available {
		return ErrUnavailable
	}
	return nil
}

// Remember, Get and Recall dispatch optional contracts. They never broaden
// capabilities based on configuration, or fall back to legacy text operations.
func Remember(ctx context.Context, p StructuredProvider, input NewRecord) (Record, error) {
	if p == nil {
		return Record{}, ErrUnavailable
	}
	writer, ok := p.(RecordWriter)
	if err := providerReady(ctx, p, ok && p.Capabilities().Remember); err != nil {
		return Record{}, err
	}
	return writer.Remember(ctx, input)
}
func Get(ctx context.Context, p StructuredProvider, owner Owner, id RecordID) (Record, error) {
	if p == nil {
		return Record{}, ErrUnavailable
	}
	reader, ok := p.(RecordReader)
	if err := providerReady(ctx, p, ok && p.Capabilities().Get); err != nil {
		return Record{}, err
	}
	return reader.Get(ctx, owner, id)
}
func Recall(ctx context.Context, p StructuredProvider, query Query) ([]Record, error) {
	if p == nil {
		return nil, ErrUnavailable
	}
	recaller, ok := p.(RecordRecaller)
	if err := providerReady(ctx, p, ok && p.Capabilities().Recall); err != nil {
		return nil, err
	}
	return recaller.Recall(ctx, query)
}

func (input NewRecord) validate() error {
	for _, value := range append([]string{input.Content, input.Source}, input.Evidence...) {
		if !utf8.ValidString(value) {
			return fmt.Errorf("%w: record content, source and evidence must be valid UTF-8", ErrInvalidInput)
		}
	}
	if err := input.Owner.validate(); err != nil {
		return err
	}
	if !knownType(input.Type) {
		return fmt.Errorf("%w: unknown knowledge type", ErrInvalidInput)
	}
	if strings.TrimSpace(input.Content) == "" {
		return fmt.Errorf("%w: content is required", ErrInvalidInput)
	}
	switch input.Layer {
	case "", LayerRaw, LayerAtomic, LayerComposite, LayerProfile:
	default:
		return fmt.Errorf("%w: unknown layer", ErrInvalidInput)
	}
	return nil
}
func knownType(kind KnowledgeType) bool {
	switch kind {
	case TypeFact, TypeDecision, TypePreference, TypeConstraint, TypeExperience, TypeProcedure, TypeSkill, TypeTask, TypeErrorSolution, TypeProjectContext:
		return true
	default:
		return false
	}
}

// Structured tokens must survive canonical JSON encoding exactly. Keep this
// validation separate from the compatibility text provider's token rules.
func validateStructuredToken(label, value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s must be valid UTF-8", label)
	}
	return validateToken(label, value)
}
