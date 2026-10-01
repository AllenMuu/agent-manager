// Package opcontext defines the read-only boundary for freshness-aware
// operational information. It does not authorize or invoke mutation tools.
package opcontext

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"time"
)

var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_.:-]{0,127}$`)

var ErrAccessDenied = errors.New("operational context read denied by access policy")

type Query struct {
	Keys []string
}

type Record struct {
	Key        string    `json:"key"`
	Source     string    `json:"source"`
	CapturedAt time.Time `json:"captured_at"`
	FreshUntil time.Time `json:"fresh_until"`
	Content    string    `json:"content,omitempty"`
}

type FreshRecord struct {
	Record
	Fresh bool `json:"fresh"`
}

type Snapshot struct {
	Available bool          `json:"available"`
	CheckedAt time.Time     `json:"checked_at"`
	Records   []FreshRecord `json:"records"`
}

// Provider is a read-only port. Implementations return source and freshness
// metadata; this package does not define write or mutation operations.
type Provider interface {
	Read(context.Context, Query) ([]Record, error)
}

// AccessPolicy is an exact allowlist for context keys. It is intentionally
// independent from the mutation policy engine and grants no tool authority.
type AccessPolicy struct {
	AllowedKeys []string
}

func (p AccessPolicy) authorize(query Query) error {
	if len(query.Keys) == 0 || len(p.AllowedKeys) == 0 {
		return ErrAccessDenied
	}
	allowed := make(map[string]struct{}, len(p.AllowedKeys))
	for _, key := range p.AllowedKeys {
		if !keyPattern.MatchString(key) {
			return fmt.Errorf("operational context access policy contains an invalid key %q", key)
		}
		if _, duplicate := allowed[key]; duplicate {
			return fmt.Errorf("operational context access policy contains duplicate key %q", key)
		}
		allowed[key] = struct{}{}
	}
	for _, key := range query.Keys {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("%w: key %q is not explicitly allowed", ErrAccessDenied, key)
		}
	}
	return nil
}

type Service struct {
	Provider     Provider
	AccessPolicy AccessPolicy
}

func (s Service) Read(ctx context.Context, query Query, now time.Time) (Snapshot, error) {
	if ctx == nil {
		return Snapshot{}, errors.New("operational context read requires a context")
	}
	if now.IsZero() {
		return Snapshot{}, errors.New("operational context read requires a check time")
	}
	if err := validateQuery(query); err != nil {
		return Snapshot{}, err
	}
	if err := s.AccessPolicy.authorize(query); err != nil {
		return Snapshot{}, err
	}
	snapshot := Snapshot{CheckedAt: now.UTC(), Records: []FreshRecord{}}
	if s.Provider == nil {
		return snapshot, nil
	}
	requested := make(map[string]struct{}, len(query.Keys))
	providerQuery := Query{Keys: append([]string(nil), query.Keys...)}
	for _, key := range query.Keys {
		requested[key] = struct{}{}
	}
	records, err := s.Provider.Read(ctx, providerQuery)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read operational context: %w", err)
	}
	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		if err := validateRecord(record); err != nil {
			return Snapshot{}, err
		}
		if _, ok := requested[record.Key]; !ok {
			return Snapshot{}, fmt.Errorf("operational context provider returned unrequested key %q", record.Key)
		}
		key := record.Source + "\x00" + record.Key
		if _, duplicate := seen[key]; duplicate {
			return Snapshot{}, errors.New("operational context provider returned duplicate source and key")
		}
		seen[key] = struct{}{}
		snapshot.Records = append(snapshot.Records, FreshRecord{Record: record, Fresh: now.Before(record.FreshUntil)})
	}
	sort.Slice(snapshot.Records, func(i, j int) bool {
		if snapshot.Records[i].Source == snapshot.Records[j].Source {
			return snapshot.Records[i].Key < snapshot.Records[j].Key
		}
		return snapshot.Records[i].Source < snapshot.Records[j].Source
	})
	snapshot.Available = true
	return snapshot, nil
}

func validateQuery(query Query) error {
	seen := make(map[string]struct{}, len(query.Keys))
	for _, key := range query.Keys {
		if !keyPattern.MatchString(key) {
			return errors.New("operational context query contains an invalid key")
		}
		if _, duplicate := seen[key]; duplicate {
			return errors.New("operational context query contains a duplicate key")
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateRecord(record Record) error {
	if !keyPattern.MatchString(record.Key) || !keyPattern.MatchString(record.Source) {
		return errors.New("operational context record requires safe key and source identifiers")
	}
	if record.CapturedAt.IsZero() || record.FreshUntil.IsZero() || record.FreshUntil.Before(record.CapturedAt) {
		return errors.New("operational context record has invalid freshness timestamps")
	}
	return nil
}
