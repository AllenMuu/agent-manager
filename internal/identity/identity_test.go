package identity_test

import (
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/identity"
)

func TestActorIdentityValidatesSafeIdentityFields(t *testing.T) {
	actor := identity.ActorIdentity{
		ID:       "user-allen",
		Kind:     identity.Human,
		Subject:  "allen@example.com",
		Provider: "local",
		Roles:    []string{"operator", "reviewer"},
	}
	if err := actor.Validate(); err != nil {
		t.Fatalf("valid actor rejected: %v", err)
	}

	actor.ID = "ghp_012345678901234567890123456789"
	if err := actor.Validate(); err == nil {
		t.Fatal("credential-like actor ID was accepted")
	}
}

func TestIdentitySelectionRequiresExplicitModeAndConsistentDelegation(t *testing.T) {
	actor := identity.ActorIdentity{ID: "user-allen", Kind: identity.Human, Subject: "allen@example.com"}
	delegation := identity.Delegation{ID: "del-1", ActorID: actor.ID, Scopes: []string{"github:read"}, ExpiresAt: time.Now().Add(time.Hour)}
	selection := identity.Selection{Mode: identity.ModeNamed, Actor: &actor, Delegation: &delegation}
	if err := selection.Validate(); err != nil {
		t.Fatalf("valid named selection rejected: %v", err)
	}

	delegation.ActorID = "user-other"
	selection.Delegation = &delegation
	if err := selection.Validate(); err == nil {
		t.Fatal("delegation linked to a different actor was accepted")
	}

	if err := (identity.Selection{}).Validate(); err == nil {
		t.Fatal("selection without an explicit mode was accepted")
	}
}

func TestAnonymousSelectionCannotCarryNamedAuthority(t *testing.T) {
	actor := identity.ActorIdentity{ID: "user-allen", Kind: identity.Human, Subject: "allen@example.com"}
	selection := identity.Selection{Mode: identity.ModeAnonymous, Actor: &actor}
	if err := selection.Validate(); err == nil {
		t.Fatal("anonymous selection carrying an actor was accepted")
	}
	if err := (identity.Selection{Mode: identity.ModeAnonymous}).Validate(); err != nil {
		t.Fatalf("explicit anonymous selection rejected: %v", err)
	}
}

func TestDelegationMatchesOnlyExactUnexpiredScopes(t *testing.T) {
	now := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	d := identity.Delegation{ID: "del-1", ActorID: "user-allen", Scopes: []string{"github:read"}, ExpiresAt: now.Add(time.Minute)}
	if err := d.Validate(); err != nil {
		t.Fatalf("valid delegation rejected: %v", err)
	}
	if !d.HasScope("github:read", now) {
		t.Fatal("exact unexpired scope was not granted")
	}
	if d.HasScope("github", now) || d.HasScope("github:read:all", now) {
		t.Fatal("scope was granted by prefix matching")
	}
	if d.HasScope("github:read", d.ExpiresAt) {
		t.Fatal("scope remained valid at its expiry time")
	}

	d.Scopes = []string{"github:*"}
	if err := d.Validate(); err == nil {
		t.Fatal("wildcard delegation scope was accepted")
	}
}

func TestDelegationBindsOnceToItsRun(t *testing.T) {
	actor := identity.ActorIdentity{ID: "user-allen", Kind: identity.Human, Subject: "allen@example.com"}
	d := identity.Delegation{ID: "del-1", ActorID: actor.ID, Scopes: []string{"github:read"}, ExpiresAt: time.Now().Add(time.Hour)}
	bound, err := d.Bind("run-1", actor)
	if err != nil {
		t.Fatalf("bind delegation: %v", err)
	}
	if bound.RunID != "run-1" || d.RunID != "" {
		t.Fatalf("Bind did not return an immutable run snapshot: input=%#v bound=%#v", d, bound)
	}
	if _, err := bound.Bind("run-2", actor); err == nil {
		t.Fatal("delegation was rebound to another run")
	}
}
