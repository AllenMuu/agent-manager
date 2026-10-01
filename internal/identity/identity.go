// Package identity defines runtime-neutral actor and delegated authority
// snapshots. It contains identifiers and safe metadata only, never credentials.
package identity

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

type ActorKind string

const (
	Human   ActorKind = "human"
	Agent   ActorKind = "agent"
	Service ActorKind = "service"
)

type Mode string

const (
	ModeNamed     Mode = "named"
	ModeAnonymous Mode = "anonymous"
	// ModeLegacyAnonymous labels pre-identity run records. New runs must not use it.
	ModeLegacyAnonymous Mode = "legacy_anonymous"
)

var (
	stableIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:@/-]{0,255}$`)
	labelPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
	scopePattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9._:/-]{0,127}$`)
	credentialText  = regexp.MustCompile(`(?i)(bearer[\s:_=-]|access[_-]?token[:=]|api[_-]?key[:=]|password[:=]|secret[:=]|token[:=])`)
)

// ActorIdentity is an allowlisted identity snapshot for one initiating actor.
type ActorIdentity struct {
	ID       string    `json:"id"`
	Kind     ActorKind `json:"kind"`
	Subject  string    `json:"subject"`
	Provider string    `json:"provider,omitempty"`
	Roles    []string  `json:"roles,omitempty"`
}

func (a ActorIdentity) Validate() error {
	if !validStableID(a.ID) {
		return errors.New("actor id must be a safe stable identifier")
	}
	switch a.Kind {
	case Human, Agent, Service:
	default:
		return fmt.Errorf("unsupported actor kind %q", a.Kind)
	}
	if !validStableID(a.Subject) {
		return errors.New("actor subject must be a safe stable identifier")
	}
	if a.Provider != "" && !validStableID(a.Provider) {
		return errors.New("actor provider must be a safe stable identifier")
	}
	if err := validateLabels("actor roles", a.Roles); err != nil {
		return err
	}
	return nil
}

// Normalized returns a copy with roles sorted for deterministic snapshots.
func (a ActorIdentity) Normalized() ActorIdentity {
	a.Roles = append([]string(nil), a.Roles...)
	sort.Strings(a.Roles)
	return a
}

// Delegation is an immutable, exact-scope authority snapshot. RunID may be
// empty before the store binds it to a newly created run.
type Delegation struct {
	ID        string    `json:"id"`
	ActorID   string    `json:"actor_id"`
	RunID     string    `json:"run_id,omitempty"`
	Scopes    []string  `json:"scopes"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (d Delegation) Validate() error {
	if !validStableID(d.ID) {
		return errors.New("delegation id must be a safe stable identifier")
	}
	if !validStableID(d.ActorID) {
		return errors.New("delegation actor id must be a safe stable identifier")
	}
	if d.RunID != "" && !validStableID(d.RunID) {
		return errors.New("delegation run id must be a safe stable identifier")
	}
	if d.ExpiresAt.IsZero() {
		return errors.New("delegation expiry is required")
	}
	if err := ValidateScopes("delegation scopes", d.Scopes); err != nil {
		return err
	}
	return nil
}

// Normalized returns a copy with exact scopes sorted for deterministic storage.
func (d Delegation) Normalized() Delegation {
	d.Scopes = append([]string(nil), d.Scopes...)
	sort.Strings(d.Scopes)
	d.ExpiresAt = d.ExpiresAt.UTC()
	return d
}

// Bind returns a run-specific snapshot without mutating the source delegation.
func (d Delegation) Bind(runID string, actor ActorIdentity) (Delegation, error) {
	if err := actor.Validate(); err != nil {
		return Delegation{}, fmt.Errorf("validate delegation actor: %w", err)
	}
	if err := d.Validate(); err != nil {
		return Delegation{}, err
	}
	if d.ActorID != actor.ID {
		return Delegation{}, errors.New("delegation actor does not match initiating actor")
	}
	if !validStableID(runID) {
		return Delegation{}, errors.New("run id must be a safe stable identifier")
	}
	if d.RunID != "" && d.RunID != runID {
		return Delegation{}, errors.New("delegation is already bound to another run")
	}
	d.RunID = runID
	return d.Normalized(), nil
}

// HasScope checks exact equality and expiry; it never applies prefix matching.
func (d Delegation) HasScope(scope string, now time.Time) bool {
	if d.ExpiresAt.IsZero() || !now.Before(d.ExpiresAt) {
		return false
	}
	for _, granted := range d.Scopes {
		if granted == scope {
			return true
		}
	}
	return false
}

type Selection struct {
	Mode       Mode           `json:"mode"`
	Actor      *ActorIdentity `json:"actor,omitempty"`
	Delegation *Delegation    `json:"delegation,omitempty"`
}

func AnonymousSelection() Selection { return Selection{Mode: ModeAnonymous} }

func NamedSelection(actor ActorIdentity, delegation Delegation) Selection {
	return Selection{Mode: ModeNamed, Actor: &actor, Delegation: &delegation}
}

func (s Selection) Validate() error {
	switch s.Mode {
	case ModeAnonymous:
		if s.Actor != nil || s.Delegation != nil {
			return errors.New("anonymous identity cannot carry actor or delegation data")
		}
	case ModeLegacyAnonymous:
		if s.Actor != nil || s.Delegation != nil {
			return errors.New("legacy anonymous identity cannot carry actor or delegation data")
		}
	case ModeNamed:
		if s.Actor == nil || s.Delegation == nil {
			return errors.New("named identity requires an actor and delegation")
		}
		if err := s.Actor.Validate(); err != nil {
			return err
		}
		if err := s.Delegation.Validate(); err != nil {
			return err
		}
		if s.Actor.ID != s.Delegation.ActorID {
			return errors.New("delegation actor does not match initiating actor")
		}
	default:
		return errors.New("identity mode must be explicitly named or anonymous")
	}
	return nil
}

func validStableID(value string) bool {
	return value == strings.TrimSpace(value) && stableIDPattern.MatchString(value) && !credentialLike(value)
}

// IsSafeReference reports whether value can be stored as a stable identity or
// delegation reference without accepting common credential-shaped strings.
func IsSafeReference(value string) bool { return validStableID(value) }

func validateLabels(field string, values []string) error {
	return validateIdentifiers(field, values, labelPattern)
}

// ValidateScopes enforces the canonical scope syntax shared with policy rules.
// Scope identifiers are exact, lowercase values and may contain path separators.
func ValidateScopes(field string, values []string) error {
	return validateIdentifiers(field, values, scopePattern)
}

// IsValidScope reports whether value uses the canonical scope syntax.
func IsValidScope(value string) bool { return scopePattern.MatchString(value) }

func validateIdentifiers(field string, values []string, pattern *regexp.Regexp) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !pattern.MatchString(value) || credentialLike(value) {
			return fmt.Errorf("%s contains an invalid or credential-like value", field)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("%s contains a duplicate value", field)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func credentialLike(value string) bool {
	lower := strings.ToLower(value)
	if credentialText.MatchString(value) {
		return true
	}
	for _, prefix := range []string{"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_", "xoxb-", "xoxp-", "xoxa-", "sk-live-", "ya29."} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return len(value) == 20 && strings.HasPrefix(value, "AKIA")
}

// IsCredentialLike reports values that resemble common credential formats or
// explicit credential assignments. It is a defensive check for identity labels.
func IsCredentialLike(value string) bool { return credentialLike(value) }
