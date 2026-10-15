package memorypipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodexAndPiProvidersDiscoverOnlyCompletedProjectSessionsAndKeepTranscriptsNative(t *testing.T) {
	project := t.TempDir()
	otherProject := t.TempDir()
	root := t.TempDir()
	codexRoot := filepath.Join(root, "codex")
	piRoot := filepath.Join(root, "pi")
	if err := os.MkdirAll(filepath.Join(codexRoot, "2026", "10", "15"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(piRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	created := "2026-10-15T12:00:00Z"
	codexCreated := "2026-10-15T12-00-00"
	writeSessionFixture(t, filepath.Join(codexRoot, "2026", "10", "15", "rollout-complete.jsonl"),
		`{"type":"session_meta","timestamp":"`+created+`","payload":{"id":"codex-complete","session_id":"codex-complete","timestamp":"`+codexCreated+`","cwd":"`+project+`"}}`,
		`{"type":"response_item","timestamp":"2026-10-15T12:01:00Z","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"private prompt secret"}]}}`,
		`{"type":"event_msg","timestamp":"2026-10-15T12:02:00Z","payload":{"type":"task_complete"}}`,
	)
	writeSessionFixture(t, filepath.Join(codexRoot, "2026", "10", "15", "rollout-active.jsonl"),
		`{"type":"session_meta","timestamp":"`+created+`","payload":{"id":"codex-active","session_id":"codex-active","timestamp":"`+codexCreated+`","cwd":"`+project+`"}}`,
		`{"type":"event_msg","timestamp":"2026-10-15T12:01:00Z","payload":{"type":"user_message","message":"still active"}}`,
	)
	writeSessionFixture(t, filepath.Join(codexRoot, "2026", "10", "15", "rollout-other.jsonl"),
		`{"type":"session_meta","timestamp":"`+created+`","payload":{"id":"codex-other","session_id":"codex-other","timestamp":"`+codexCreated+`","cwd":"`+otherProject+`"}}`,
		`{"type":"event_msg","timestamp":"2026-10-15T12:02:00Z","payload":{"type":"task_complete"}}`,
	)
	piPath := filepath.Join(piRoot, "--project--", "20261015_pi-complete.jsonl")
	writeSessionFixture(t, piPath,
		`{"type":"session","version":3,"id":"pi-complete","timestamp":"`+created+`","cwd":"`+project+`"}`,
		`{"type":"message","id":"user0001","parentId":null,"timestamp":"2026-10-15T12:01:00Z","message":{"role":"user","content":[{"type":"text","text":"private Pi prompt secret"}]}}`,
		`{"type":"message","id":"agent001","parentId":"user0001","timestamp":"2026-10-15T12:02:00Z","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"private answer secret"}]}}`,
	)

	codex, err := NewCodexRolloutProvider(codexRoot, "registered-project", project)
	if err != nil {
		t.Fatal(err)
	}
	pi, err := NewPiSessionProvider(piRoot, "registered-project", project)
	if err != nil {
		t.Fatal(err)
	}
	codexRefs, err := codex.Discover(context.Background(), "registered-project")
	if err != nil || len(codexRefs) != 1 || codexRefs[0].NativeRunID != "codex-complete" || codexRefs[0].SourceRef != "codex://sessions/codex-complete" {
		t.Fatalf("Codex discoveries = %#v, %v", codexRefs, err)
	}
	wantCodexCreated := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	if !codexRefs[0].CreatedAt.Equal(wantCodexCreated) {
		t.Fatalf("Codex metadata timestamp = %s, want %s", codexRefs[0].CreatedAt, wantCodexCreated)
	}
	piRefs, err := pi.Discover(context.Background(), "registered-project")
	if err != nil || len(piRefs) != 1 || piRefs[0].NativeRunID != "pi-complete" || piRefs[0].SourceRef != "pi://sessions/pi-complete" {
		t.Fatalf("Pi discoveries = %#v, %v", piRefs, err)
	}
	for _, providerAndRef := range []struct {
		provider *NativeSessionProvider
		ref      RunEvidenceRef
	}{{codex, codexRefs[0]}, {pi, piRefs[0]}} {
		evidence, err := providerAndRef.provider.Read(context.Background(), providerAndRef.ref)
		if err != nil || evidence.SourceDigest == "" || strings.Contains(evidence.SummaryText, "secret") || len(evidence.Discoveries) != 0 {
			t.Fatalf("native source leaked transcript or lost content digest: %#v, %v", evidence, err)
		}
	}
	if err := os.WriteFile(piPath, []byte(`{"type":"session","version":3,"id":"pi-complete","timestamp":"`+created+`","cwd":"`+project+`"}`+"\n"+
		`{"type":"message","id":"user0001","parentId":null,"timestamp":"2026-10-15T12:01:00Z","message":{"role":"user"}}`+"\n"+
		`{"type":"message","id":"agent001","parentId":"user0001","timestamp":"2026-10-15T12:02:00Z","message":{"role":"assistant","stopReason":"stop"}}`+"\n"+
		`{"type":"custom","id":"annotation","timestamp":"2026-10-15T12:03:00Z"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := pi.Read(context.Background(), piRefs[0]); !errors.Is(err, ErrEvidenceChanged) {
		t.Fatalf("changed native evidence error = %v", err)
	}
	if _, err := codex.Discover(context.Background(), "another-project"); !errors.Is(err, ErrEvidenceNotFound) {
		t.Fatalf("foreign project discovery error = %v", err)
	}
}

func TestNativeProvidersRejectUnsafeAndOversizedSources(t *testing.T) {
	project := t.TempDir()
	root := t.TempDir()
	provider, err := NewCodexRolloutProvider(root, "registered-project", project)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "rollout-invalid.jsonl"), []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Discover(context.Background(), "registered-project"); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatalf("malformed source error = %v", err)
	}
	if _, err := readNativeSessionFile(filepath.Join(root, "rollout-invalid.jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "rollout-invalid.jsonl")); err != nil {
		t.Fatal(err)
	}
	large := filepath.Join(root, "rollout-large.jsonl")
	if err := os.WriteFile(large, []byte(strings.Repeat("x", maxNativeSessionBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readNativeSessionFile(large); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatalf("oversized source error = %v", err)
	}
	if _, err := NewPiSessionProvider(filepath.Join(root, "missing"), "registered-project", project); err == nil {
		t.Fatal("missing session directory was accepted")
	}
	if _, err := parseNativeTimestamp(time.Time{}.Format(time.RFC3339)); err == nil {
		t.Fatal("zero timestamp was accepted")
	}
}

func writeSessionFixture(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}
