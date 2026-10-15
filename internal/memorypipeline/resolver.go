package memorypipeline

import (
	"context"
	"errors"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/memory"
)

type Resolver struct {
	projectID string
	store     *Store
	gateway   *memory.Gateway
	provider  RunEvidenceProvider
}

type InitialRetrieval struct {
	Summary   *ProjectSummary `json:"summary,omitempty"`
	Canonical []memory.Record `json:"canonical"`
	Derived   []SearchResult  `json:"derived,omitempty"`
}

func NewResolver(projectID string, store *Store, gateway *memory.Gateway, provider RunEvidenceProvider) (*Resolver, error) {
	if err := validateProjectID(projectID); err != nil {
		return nil, err
	}
	if store == nil {
		return nil, ErrInvalidRequest
	}
	return &Resolver{projectID: projectID, store: store, gateway: gateway, provider: provider}, nil
}

func (r *Resolver) Summary(ctx context.Context, owner memory.Owner) (ProjectSummary, error) {
	if err := r.authorize(owner); err != nil {
		return ProjectSummary{}, err
	}
	return r.store.Summary(ctx, r.projectID)
}

func (r *Resolver) CanonicalSearch(ctx context.Context, owner memory.Owner, query string) ([]memory.Record, error) {
	if err := r.authorize(owner); err != nil {
		return nil, err
	}
	if strings.TrimSpace(query) == "" {
		return []memory.Record{}, nil
	}
	if r.gateway == nil {
		return nil, memory.ErrUnavailable
	}
	return r.gateway.Search(ctx, memory.SearchRequest{Owner: owner, Text: query})
}

func (r *Resolver) DerivedSearch(ctx context.Context, owner memory.Owner, query string, limit int) ([]SearchResult, error) {
	if err := r.authorize(owner); err != nil {
		return nil, err
	}
	return r.store.Search(ctx, r.projectID, query, limit)
}

func (r *Resolver) Evidence(ctx context.Context, owner memory.Owner, evidenceID string) (RunEvidence, error) {
	if err := r.authorize(owner); err != nil {
		return RunEvidence{}, err
	}
	if r.provider == nil {
		return RunEvidence{}, ErrEvidenceNotFound
	}
	ref, err := r.store.Evidence(ctx, r.projectID, evidenceID)
	if err != nil {
		return RunEvidence{}, err
	}
	evidence, err := r.provider.Read(ctx, ref)
	if err != nil {
		return RunEvidence{}, err
	}
	computed, err := referenceFor(evidence)
	if err != nil {
		return RunEvidence{}, err
	}
	if !sameReference(ref, computed) || !sameReference(ref, evidence.Ref) {
		return RunEvidence{}, ErrEvidenceChanged
	}
	return evidence, nil
}

// Initial returns only the compact summary and bounded canonical results.
// Derived records and raw evidence are intentionally absent until requested.
func (r *Resolver) Initial(ctx context.Context, owner memory.Owner, query string) (InitialRetrieval, error) {
	if err := r.authorize(owner); err != nil {
		return InitialRetrieval{}, err
	}
	result := InitialRetrieval{Canonical: []memory.Record{}, Derived: []SearchResult{}}
	summary, err := r.Summary(ctx, owner)
	if err == nil {
		result.Summary = &summary
	} else if !errors.Is(err, ErrRecordNotFound) {
		return InitialRetrieval{}, err
	}
	canonical, err := r.CanonicalSearch(ctx, owner, query)
	if err != nil {
		return result, err
	}
	result.Canonical = canonical
	return result, nil
}

func (r *Resolver) ProviderStatus(ctx context.Context, owner memory.Owner) (memory.ProviderStatus, error) {
	if err := r.authorize(owner); err != nil {
		return memory.ProviderStatus{}, err
	}
	if r.gateway == nil {
		return memory.ProviderStatus{Reason: memory.ErrUnavailable.Error()}, nil
	}
	return r.gateway.ProviderStatus(ctx), nil
}

func (r *Resolver) authorize(owner memory.Owner) error {
	if r == nil || owner.Kind != memory.OwnerProject || owner.ProjectID != r.projectID || owner.UserID != "" || owner.AgentID != "" || owner.SessionID != "" {
		return memory.ErrOwnershipDenied
	}
	return nil
}
