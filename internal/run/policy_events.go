package run

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/AllenMuu/skill-manager/internal/enforcement"
	"github.com/AllenMuu/skill-manager/internal/identity"
)

// PolicyEventAdapter establishes source trust independently of runtime payload.
// Only the trusted host installs it. A successful return carries the adapter's
// established evidence; payload source/trusted labels are never consulted here.
type PolicyEventAdapter interface {
	EstablishPolicyEvent(context.Context, PolicyEvent) (EstablishedPolicyEvent, error)
}
type PolicyEvent struct {
	ID      string
	Payload []byte
}
type EstablishedPolicyEvent struct {
	Receipt  enforcement.PolicyReceipt `json:"receipt"`
	Origin   string                    `json:"origin"`
	Trust    string                    `json:"trust"`
	SourceID string                    `json:"source_id"`
}
type PolicyEventObservation struct {
	ID       string `json:"id"`
	EventID  string `json:"event_id,omitempty"`
	SourceID string `json:"source_id"`
	Origin   string `json:"origin"`
	Trust    string `json:"trust"`
	Outcome  string `json:"outcome"`
}
type PolicyEventStore interface {
	RecordPolicyEvent(string, EstablishedPolicyEvent) (Record, error)
}

func (c *Coordinator) ReceivePolicyEvent(ctx context.Context, id string, raw PolicyEvent) (Record, error) {
	if err := checkContext(ctx); err != nil {
		return Record{}, err
	}
	c.policyMu.Lock()
	adapter := c.policyEvents
	c.policyMu.Unlock()
	if adapter == nil {
		return Record{}, enforcement.ErrUnsupported
	}
	store, ok := c.store.(PolicyEventStore)
	if !ok {
		return Record{}, enforcement.ErrUnsupported
	}
	raw.Payload = append([]byte(nil), raw.Payload...)
	event, err := adapter.EstablishPolicyEvent(ctx, raw)
	if err != nil {
		return Record{}, errors.New("policy event source could not be established")
	}
	if err = ctx.Err(); err != nil {
		return Record{}, err
	}
	data, err := json.Marshal(event)
	if err != nil {
		return Record{}, errors.New("invalid adapter evidence")
	}
	var copied EstablishedPolicyEvent
	if err = json.Unmarshal(data, &copied); err != nil {
		return Record{}, errors.New("invalid adapter evidence")
	}
	return store.RecordPolicyEvent(id, copied)
}
func (s *Store) RecordPolicyEvent(id string, event EstablishedPolicyEvent) (Record, error) {
	var result Record
	err := s.update(func(db *database) error {
		r, ok := db.Runs[id]
		if !ok || r.PolicyRevisions == nil {
			return errors.New("revision missing")
		}
		h := r.PolicyRevisions
		m := &h.Mutations[len(h.Mutations)-1]
		if !identity.IsSafeReference(event.SourceID) || !validPolicyOrigin(event.Origin) || (event.Trust != "established" && event.Trust != "untrusted") {
			return errors.New("invalid adapter source evidence")
		}
		eventID := event.Receipt.EventID
		if !identity.IsSafeReference(eventID) {
			eventID = ""
		}
		observationID, err := newID("policy-event")
		if err != nil {
			return err
		}
		observation := PolicyEventObservation{ID: observationID, EventID: eventID, SourceID: event.SourceID, Origin: event.Origin, Trust: event.Trust}
		switch {
		case event.Trust != "established":
			observation.Outcome = "untrusted"
		case validatePolicyReceipt(m.Request, event.Receipt) != nil:
			observation.Outcome = "stale"
		case m.State == "applied":
			observation.Outcome = "duplicate"
		case event.Receipt.State == "accepted":
			observation.Outcome = "accepted"
		default:
			observation.Outcome = "applied"
			h.Applied = m.Request.Target
			m.State = "applied"
			m.Confirmation = &event.Receipt
			m.ConfirmationOrigin = event.Origin
			m.ConfirmationTrust = event.Trust
		}
		// Repeated established evidence is idempotent; retain the first diagnostic.
		for _, prior := range h.Events {
			if prior.EventID == observation.EventID && prior.SourceID == observation.SourceID && prior.Origin == observation.Origin && prior.Trust == observation.Trust && prior.Outcome == observation.Outcome {
				result = r
				return nil
			}
		}
		h.Events = append(h.Events, observation)
		db.Runs[id] = r
		result = r
		return nil
	})
	return result, err
}
func validPolicyOrigin(origin string) bool {
	return origin == "agent" || origin == "runtime" || origin == "control-plane" || origin == "infrastructure"
}
