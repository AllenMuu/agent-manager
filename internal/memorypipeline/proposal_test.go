package memorypipeline

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/memoryintegration"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type receiptMemoryProvider struct {
	*memory.InMemoryProvider
	mu           sync.Mutex
	receipts     map[string]memory.Record
	calls        int
	unknown      bool
	beforeCommit func(string)
}

func newReceiptMemoryProvider() *receiptMemoryProvider {
	return &receiptMemoryProvider{InMemoryProvider: memory.NewInMemoryProvider(), receipts: map[string]memory.Record{}}
}

func (p *receiptMemoryProvider) RememberWithOperation(ctx context.Context, request memory.RememberRequest) (memory.Record, error) {
	p.mu.Lock()
	p.calls++
	if existing, ok := p.receipts[request.OperationID]; ok {
		p.mu.Unlock()
		return existing, nil
	}
	unknown, before := p.unknown, p.beforeCommit
	p.mu.Unlock()
	if before != nil {
		before(request.OperationID)
	}
	if unknown {
		return memory.Record{}, memory.ErrOutcomeUnknown
	}
	created, err := p.InMemoryProvider.Remember(ctx, request.Record)
	if err != nil {
		return memory.Record{}, err
	}
	p.mu.Lock()
	p.receipts[request.OperationID] = created
	p.mu.Unlock()
	return created, nil
}

func (p *receiptMemoryProvider) callsMade() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func proposalFixture(t *testing.T) (*Store, RunEvidenceRef, Proposal, *receiptMemoryProvider, *memory.Gateway, memory.Owner) {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	ref := storeEvidenceRef(t, "project-proposal", "run-proposal", "proposal")
	if _, err := store.ImportEvidence(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if err := store.Consolidate(context.Background(), storeExtraction(ref, "The cache key includes the project identity.")); err != nil {
		t.Fatal(err)
	}
	items, err := store.Search(context.Background(), ref.ProjectID, "cache key project", 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("prepared derived memory = %#v, %v", items, err)
	}
	proposal, err := store.Propose(context.Background(), ref.ProjectID, items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	provider := newReceiptMemoryProvider()
	owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: ref.ProjectID}
	gateway, err := memory.NewGateway(provider, memory.Access{ReadOwners: []memory.Owner{owner}, WriteOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	return store, ref, proposal, provider, gateway, owner
}

func TestProposalCreationRetainsEvidenceWithoutWritingCanonicalMemory(t *testing.T) {
	store, ref, proposal, provider, gateway, owner := proposalFixture(t)
	defer store.Close()
	if proposal.Status != ProposalPending || proposal.OperationID == "" || len(proposal.EvidenceIDs) != 1 || proposal.EvidenceIDs[0] != ref.ID {
		t.Fatalf("proposal provenance = %#v", proposal)
	}
	if provider.callsMade() != 0 {
		t.Fatal("proposal creation wrote canonical Memory")
	}
	results, err := gateway.Search(context.Background(), memory.SearchRequest{Owner: owner, Text: "cache key project"})
	if err != nil || len(results) != 0 {
		t.Fatalf("pending proposal appeared in canonical search: %#v, %v", results, err)
	}
}

func TestConsolidationCreatesOnlyEligibleProposalsAndMergesPendingEvidence(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	first := storeEvidenceRef(t, "project-auto-proposals", "run-decision-one", "one")
	second := storeEvidenceRef(t, "project-auto-proposals", "run-decision-two", "two")
	fact := storeEvidenceRef(t, "project-auto-proposals", "run-fact", "fact")
	for _, ref := range []RunEvidenceRef{first, second, fact} {
		if _, err := store.ImportEvidence(ctx, ref); err != nil {
			t.Fatal(err)
		}
	}
	decision := storeExtraction(first, "The canonical cache key includes the project identity.")
	decision.Memories[0].Field = FieldDecision
	decision.Memories[0].Type = memory.TypeDecision
	if err := store.Consolidate(ctx, decision); err != nil {
		t.Fatal(err)
	}
	decision = storeExtraction(second, "The canonical cache key includes the project identity.")
	decision.Memories[0].Field = FieldDecision
	decision.Memories[0].Type = memory.TypeDecision
	if err := store.Consolidate(ctx, decision); err != nil {
		t.Fatal(err)
	}
	if err := store.Consolidate(ctx, storeExtraction(fact, "A cache key was observed.")); err != nil {
		t.Fatal(err)
	}
	proposals, err := store.ListProposals(ctx, first.ProjectID)
	if err != nil || len(proposals) != 1 {
		t.Fatalf("eligible proposals = %#v, %v", proposals, err)
	}
	proposal := proposals[0]
	if proposal.Status != ProposalPending || len(proposal.EvidenceIDs) != 2 || proposal.EvidenceIDs[0] == proposal.EvidenceIDs[1] {
		t.Fatalf("pending proposal did not merge the complete evidence lineage: %#v", proposal)
	}
	if derived, err := store.GetDerived(ctx, first.ProjectID, proposal.DerivedID); err != nil || derived.Type != memory.TypeDecision {
		t.Fatalf("proposal references unexpected derived memory: %#v, %v", derived, err)
	}
}

func TestAcceptProposalUsesGatewayAndPersistsOperationBeforeCommit(t *testing.T) {
	store, ref, proposal, provider, gateway, owner := proposalFixture(t)
	defer store.Close()
	provider.beforeCommit = func(operationID string) {
		current, err := store.Proposal(context.Background(), ref.ProjectID, proposal.ID)
		if err != nil || current.Status != ProposalPublishing || current.OperationID != operationID {
			t.Errorf("publication intent not durable before Gateway write: %#v, %v", current, err)
		}
	}
	prepared, err := store.PreviewProposalAcceptance(context.Background(), ref.ProjectID, proposal.ID, gateway, owner)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Plan.Operation != memory.OperationAdd || prepared.Plan.OperationID != proposal.OperationID || prepared.Plan.Owner != owner {
		t.Fatalf("acceptance preview = %#v", prepared.Plan)
	}
	accepted, err := store.ConfirmProposalAcceptance(context.Background(), prepared, true)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Status != ProposalAccepted || accepted.CanonicalRecordID == "" || accepted.OperationID != proposal.OperationID || provider.callsMade() != 1 {
		t.Fatalf("accepted proposal = %#v calls=%d", accepted, provider.callsMade())
	}
	canonical, err := gateway.Get(context.Background(), owner, memory.RecordID(accepted.CanonicalRecordID))
	if err != nil {
		t.Fatal(err)
	}
	if canonical.Source != "experience-proposal-"+proposal.ID || len(canonical.Evidence) != 1 || canonical.Evidence[0] != "agent-manager://runs/"+ref.NativeRunID+"#"+ref.ContentHash {
		t.Fatalf("canonical record lost proposal or evidence lineage: %#v", canonical)
	}
	readOnlyGateway, err := memory.NewGateway(provider, memory.Access{ReadOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	server, err := memoryintegration.NewServer(readOnlyGateway, memoryintegration.SessionConfig{Agent: "codex", Owner: owner})
	if err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	clientSession, err := mcp.NewClient(&mcp.Implementation{Name: "proposal-acceptance-test", Version: "1"}, nil).Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()
	agentRead, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{Name: "memory_get", Arguments: map[string]any{"owner": owner, "id": canonical.ID}})
	if err != nil || agentRead.IsError {
		t.Fatalf("separate agent integration could not read accepted Memory: %+v %v", agentRead, err)
	}
	var integrationRecord memory.Record
	if err := json.Unmarshal([]byte(agentRead.Content[0].(*mcp.TextContent).Text), &integrationRecord); err != nil {
		t.Fatal(err)
	}
	if integrationRecord.ID != canonical.ID || integrationRecord.Evidence[0] != refEvidence(ref) {
		t.Fatalf("agent integration read changed canonical lineage: %#v", integrationRecord)
	}
	if _, err := store.ConfirmProposalAcceptance(context.Background(), prepared, true); err == nil {
		t.Fatal("reused confirmation preview published a second time")
	}
	if provider.callsMade() != 1 {
		t.Fatal("accepted proposal was automatically published again")
	}
}

func TestRejectedProposalAndUnauthorizedAcceptDoNotPublish(t *testing.T) {
	store, ref, proposal, provider, gateway, owner := proposalFixture(t)
	defer store.Close()
	unauthorized, err := memory.NewGateway(provider, memory.Access{ReadOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PreviewProposalAcceptance(context.Background(), ref.ProjectID, proposal.ID, unauthorized, owner); !errors.Is(err, memory.ErrOwnershipDenied) {
		t.Fatalf("unauthorized Gateway accepted a proposal: %v", err)
	}
	pending, err := store.Proposal(context.Background(), ref.ProjectID, proposal.ID)
	if err != nil || pending.Status != ProposalPending || pending.OperationID != proposal.OperationID {
		t.Fatalf("unauthorized attempt changed proposal: %#v, %v", pending, err)
	}
	rejected, err := store.RejectProposal(context.Background(), ref.ProjectID, proposal.ID, true)
	if err != nil || rejected.Status != ProposalRejected {
		t.Fatalf("reject: %#v, %v", rejected, err)
	}
	results, err := gateway.Search(context.Background(), memory.SearchRequest{Owner: owner, Text: "cache key project"})
	if err != nil || len(results) != 0 || provider.callsMade() != 0 {
		t.Fatalf("rejection changed canonical Memory: %#v %v calls=%d", results, err, provider.callsMade())
	}
}

func TestUnknownGatewayOutcomeIsReconciledWithoutReplay(t *testing.T) {
	store, ref, proposal, provider, gateway, owner := proposalFixture(t)
	defer store.Close()
	provider.unknown = true
	prepared, err := store.PreviewProposalAcceptance(context.Background(), ref.ProjectID, proposal.ID, gateway, owner)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.ConfirmProposalAcceptance(context.Background(), prepared, true)
	if !errors.Is(err, memory.ErrOutcomeUnknown) || first.Status != ProposalReconcileRequired {
		t.Fatalf("unknown outcome = %#v, %v", first, err)
	}
	if _, err := store.PreviewProposalAcceptance(context.Background(), ref.ProjectID, proposal.ID, gateway, owner); !errors.Is(err, ErrPublicationUncertain) {
		t.Fatalf("unknown publication was retried: %v", err)
	}
	current, err := store.ReconcileProposal(context.Background(), ref.ProjectID, proposal.ID, gateway, owner)
	if err != nil || current.Status != ProposalReconcileRequired || provider.callsMade() != 1 {
		t.Fatalf("reconcile must remain inspectable without replay: %#v err=%v calls=%d", current, err, provider.callsMade())
	}
}

func refEvidence(ref RunEvidenceRef) string { return ref.SourceRef + "#" + ref.ContentHash }
