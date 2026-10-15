package memorypipeline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/identity"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/memoryintegration"
	"github.com/AllenMuu/skill-manager/internal/policy"
	"github.com/AllenMuu/skill-manager/internal/run"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestVerticalSliceFromRunEvidenceToReviewedCanonicalMemory(t *testing.T) {
	ctx := context.Background()
	projectRoot := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(projectRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	runStore, err := run.NewStore(filepath.Join(t.TempDir(), "runs"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := run.NewManager(runStore)
	if err != nil {
		t.Fatal(err)
	}
	startedAt := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	runRecord, _, err := manager.Start(evidenceSnapshot(t), "codex", projectRoot, identity.AnonymousSelection(), startedAt, evidenceController{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := manager.EvaluateAndRecord(runRecord.ID, policy.Event{Category: policy.ToolCallRequested, Tool: "read_file"}, policy.BudgetState{}, startedAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Kill(ctx, runRecord.ID, run.TerminationOperatorRequested, startedAt.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	provider, err := NewAgentManagerRunProvider(runStore, "project-vertical", projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := provider.Discover(ctx, "project-vertical")
	if err != nil || len(refs) != 1 || refs[0].Status != ImportCompleted {
		t.Fatalf("terminal run reference = %#v, %v", refs, err)
	}
	store, err := InitializeProject(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ref, err := store.ImportEvidence(ctx, refs[0])
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := provider.Read(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	firstExtraction, err := ExtractEvidence(evidence)
	if err != nil {
		t.Fatal(err)
	}
	secondExtraction, err := ExtractEvidence(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if firstExtraction.Summary != secondExtraction.Summary || len(firstExtraction.Memories) == 0 {
		t.Fatalf("extraction was not deterministic or yielded no derived evidence: %#v / %#v", firstExtraction, secondExtraction)
	}
	if err := store.Consolidate(ctx, firstExtraction); err != nil {
		t.Fatal(err)
	}
	summary, err := store.Summary(ctx, "project-vertical")
	if err != nil || !strings.Contains(summary.Content, firstExtraction.Summary) || len(summary.EvidenceIDs) != 1 || summary.EvidenceIDs[0] != ref.ID {
		t.Fatalf("persisted project summary lost deterministic source lineage: %#v, %v", summary, err)
	}
	derived, err := store.Search(ctx, "project-vertical", "policy decision", 10)
	if err != nil || len(derived) == 0 || derived[0].Verification != VerificationUnverified || len(derived[0].EvidenceIDs) != 1 || derived[0].EvidenceIDs[0] != ref.ID {
		t.Fatalf("derived search or trust boundary = %#v, %v", derived, err)
	}
	owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: "project-vertical"}
	canonicalProvider := newReceiptMemoryProvider()
	gateway, err := memory.NewGateway(canonicalProvider, memory.Access{ReadOwners: []memory.Owner{owner}, WriteOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	before, err := gateway.Search(ctx, memory.SearchRequest{Owner: owner, Text: "policy decision"})
	if err != nil || len(before) != 0 {
		t.Fatalf("raw history became canonical before review: %#v, %v", before, err)
	}
	proposal, err := store.Propose(ctx, owner.ProjectID, derived[0].ID)
	if err != nil || proposal.Status != ProposalPending || len(proposal.EvidenceIDs) != 1 {
		t.Fatalf("provenance-linked proposal = %#v, %v", proposal, err)
	}
	prepared, err := store.PreviewProposalAcceptance(ctx, owner.ProjectID, proposal.ID, gateway, owner)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := store.ConfirmProposalAcceptance(ctx, prepared, true)
	if err != nil || accepted.Status != ProposalAccepted || accepted.CanonicalRecordID == "" {
		t.Fatalf("reviewed canonical acceptance = %#v, %v", accepted, err)
	}

	readOnlyGateway, err := memory.NewGateway(canonicalProvider, memory.Access{ReadOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	server, err := memoryintegration.NewServer(readOnlyGateway, memoryintegration.SessionConfig{Agent: "codex", Owner: owner})
	if err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	clientSession, err := mcp.NewClient(&mcp.Implementation{Name: "vertical-memory-test", Version: "1"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()
	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "memory_get", Arguments: map[string]any{"owner": owner, "id": accepted.CanonicalRecordID}})
	if err != nil || result.IsError {
		t.Fatalf("separate authorized agent could not read the accepted canonical record: %+v, %v", result, err)
	}
	var record memory.Record
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &record); err != nil {
		t.Fatal(err)
	}
	if record.Source != "experience-proposal-"+proposal.ID || len(record.Evidence) != 1 || record.Evidence[0] != refEvidence(ref) {
		t.Fatalf("canonical record lost reviewed provenance: %#v", record)
	}
}
