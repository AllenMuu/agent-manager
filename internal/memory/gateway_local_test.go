//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package memory_test

import (
	"context"
	"errors"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestOwnedMutationPreviewRequiresExactConfirmation(t *testing.T) {
	ctx := context.Background()
	store, err := memory.OpenStructuredStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: "project-one"}
	gateway, err := memory.NewGateway(store, memory.Access{ReadOwners: []memory.Owner{owner}, WriteOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	input := memory.Mutation{Operation: memory.OperationAdd, Record: memory.NewRecord{Owner: owner, Type: memory.TypeFact, Content: "confirmed fact", Source: "operator", Evidence: []string{"issue:21"}}}
	preview, err := gateway.Preview(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = gateway.Commit(ctx, preview, memory.Confirmation{}); !errors.Is(err, memory.ErrNotConfirmed) {
		t.Fatalf("unconfirmed write: %v", err)
	}
	results, err := memory.Recall(ctx, store, memory.Query{Owner: owner})
	if err != nil || len(results) != 0 {
		t.Fatalf("preview mutated store: %v %v", results, err)
	}
	input.Record.Content = "swapped"
	input.Record.Evidence[0] = "swapped"
	confirmation := memory.Confirmation{Confirmed: true, IntentID: preview.IntentID(), Owner: owner}
	records, err := gateway.Commit(ctx, preview, confirmation)
	if err != nil || len(records) != 1 || records[0].Content != "confirmed fact" || records[0].Evidence[0] != "issue:21" {
		t.Fatalf("confirmed original intent: %v %v", records, err)
	}
}

func TestProjectIdentityIsDistinctAndRelocationIsExplicit(t *testing.T) {
	root := t.TempDir()
	registry, err := memory.OpenProjectRegistry(root + "/projects.json")
	if err != nil {
		t.Fatal(err)
	}
	first := t.TempDir() + "/repo"
	second := t.TempDir() + "/repo"
	moved := t.TempDir() + "/relocated"
	for _, path := range []string{first, second, moved} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = registry.Register(context.Background(), first, false); !errors.Is(err, memory.ErrNotConfirmed) {
		t.Fatal(err)
	}
	a, err := registry.Register(context.Background(), first, true)
	if err != nil {
		t.Fatal(err)
	}
	b, err := registry.Register(context.Background(), second, true)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID || a.ID == "repo" {
		t.Fatalf("basename identity: %v %v", a, b)
	}
	if _, err = registry.Lookup(moved); !errors.Is(err, memory.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err = registry.Relocate(context.Background(), a.ID, moved, false); !errors.Is(err, memory.ErrNotConfirmed) {
		t.Fatal(err)
	}
	if _, err = registry.Relocate(context.Background(), a.ID, second, true); !errors.Is(err, memory.ErrConflict) {
		t.Fatal(err)
	}
	relocated, err := registry.Relocate(context.Background(), a.ID, moved, true)
	if err != nil || relocated.ID != a.ID {
		t.Fatalf("relocation: %v %v", relocated, err)
	}
	reopened, err := memory.OpenProjectRegistry(root + "/projects.json")
	if err != nil {
		t.Fatal(err)
	}
	found, err := reopened.Lookup(moved)
	if err != nil || found.ID != a.ID {
		t.Fatalf("reopened: %v %v", found, err)
	}
	if _, err = reopened.Lookup(first); !errors.Is(err, memory.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestConfirmedLifecycleKeepsInspectableRetiredLineage(t *testing.T) {
	ctx := context.Background()
	store, err := memory.OpenStructuredStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: "lifecycle"}
	gateway, err := memory.NewGateway(store, memory.Access{ReadOwners: []memory.Owner{owner}, WriteOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	commit := func(m memory.Mutation) memory.Record {
		t.Helper()
		p, e := gateway.Preview(ctx, m)
		if e != nil {
			t.Fatal(e)
		}
		rs, e := gateway.Commit(ctx, p, memory.Confirmation{Confirmed: true, IntentID: p.IntentID(), Owner: owner})
		if e != nil {
			t.Fatal(e)
		}
		return rs[0]
	}
	original := commit(memory.Mutation{Operation: memory.OperationAdd, Record: memory.NewRecord{Owner: owner, Type: memory.TypeDecision, Content: "v1", Source: "design"}})
	updated := commit(memory.Mutation{Operation: memory.OperationUpdate, ID: original.ID, ExpectedVersion: 1, Record: memory.NewRecord{Owner: owner, Type: memory.TypeDecision, Content: "v2", Source: "design"}})
	if updated.ID != original.ID || updated.Version != 2 {
		t.Fatal(updated)
	}
	replacement := commit(memory.Mutation{Operation: memory.OperationSupersede, ID: original.ID, ExpectedVersion: 2, Record: memory.NewRecord{Owner: owner, Type: memory.TypeDecision, Content: "replacement", Source: "design"}})
	retired, err := gateway.Get(ctx, owner, original.ID)
	if err != nil || retired.State != memory.RecordSuperseded || retired.SupersededBy != replacement.ID {
		t.Fatalf("retired: %v %v", retired, err)
	}
	deleted := commit(memory.Mutation{Operation: memory.OperationForget, ID: replacement.ID, ExpectedVersion: 1, Record: memory.NewRecord{Owner: owner}})
	history, err := gateway.History(ctx, owner, replacement.ID)
	if err != nil || len(history) != 2 || deleted.State != memory.RecordDeleted {
		t.Fatalf("history: %v %v", history, err)
	}
}

func TestImportConfirmationBindsOwnerSourceAndExactPreviewedContent(t *testing.T) {
	ctx := context.Background()
	store, err := memory.OpenStructuredStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	owner := memory.Owner{Kind: memory.OwnerUser, UserID: "operator"}
	g, err := memory.NewGateway(store, memory.Access{ReadOwners: []memory.Owner{owner}, WriteOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/legacy.txt"
	if err = os.WriteFile(path, []byte("old lesson\n"), 0600); err != nil {
		t.Fatal(err)
	}
	input := memory.Mutation{Operation: memory.OperationImport, Record: memory.NewRecord{Owner: owner}, Import: memory.LegacyImportRequest{Path: path, Owner: owner, Source: "declared-source", Type: memory.TypeExperience}}
	preview, err := g.Preview(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	confirm := memory.Confirmation{Confirmed: true, IntentID: preview.IntentID(), Owner: owner, Source: "wrong-source"}
	if _, err = g.Commit(ctx, preview, confirm); !errors.Is(err, memory.ErrNotConfirmed) {
		t.Fatal(err)
	}
	confirm.Source = "declared-source"
	if err = os.WriteFile(path, []byte("changed lesson\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = g.Commit(ctx, preview, confirm); !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("changed import: %v", err)
	}
	rs, err := memory.Recall(ctx, store, memory.Query{Owner: owner})
	if err != nil || len(rs) != 0 {
		t.Fatalf("unconfirmed import mutated: %v %v", rs, err)
	}
	preview, err = g.Preview(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	confirm.IntentID = preview.IntentID()
	imported, err := g.Commit(ctx, preview, confirm)
	if err != nil || len(imported) != 1 || imported[0].Content != "changed lesson" || imported[0].Source != "declared-source" {
		t.Fatalf("import: %v %v", imported, err)
	}
}

func TestSearchSelectsBoundedAttributedCurrentRecordsDeterministically(t *testing.T) {
	ctx := context.Background()
	store, err := memory.OpenStructuredStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: "bounded"}
	inputs := []memory.NewRecord{
		{Owner: owner, Type: memory.TypeFact, Content: "alpha beta", Source: "primary", Evidence: []string{"test:both"}},
		{Owner: owner, Type: memory.TypeFact, Content: "alpha", Source: "secondary", Evidence: []string{"test:one"}},
		{Owner: owner, Type: memory.TypeFact, Content: "alpha", Source: "secondary", Evidence: []string{"test:two"}},
		{Owner: owner, Type: memory.TypeDecision, Content: "alpha beta", Source: "wrong-type"},
		{Owner: owner, Type: memory.TypeFact, Content: "alpha beta", Source: "retired"},
	}
	records := []memory.Record{}
	for _, input := range inputs {
		r, e := memory.Remember(ctx, store, input)
		if e != nil {
			t.Fatal(e)
		}
		records = append(records, r)
	}
	_, err = memory.Forget(ctx, store, memory.MutationRequest{Owner: owner, ID: records[4].ID, ExpectedVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	g, err := memory.NewGateway(store, memory.Access{ReadOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{MaxResults: 2, ContentBytes: 15, ContextBytes: 1000, MinRelevance: .5})
	if err != nil {
		t.Fatal(err)
	}
	request := memory.SearchRequest{Owner: owner, Text: "alpha beta", Type: memory.TypeFact}
	found, err := g.Search(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	expectedTie := records[1].ID
	if records[2].ID < expectedTie {
		expectedTie = records[2].ID
	}
	if len(found) != 2 || found[0].ID != records[0].ID || found[1].ID != expectedTie || found[0].Source != "primary" || found[0].Evidence[0] != "test:both" {
		t.Fatalf("selected: %v", found)
	}
	for i := 0; i < 3; i++ {
		again, e := g.Search(ctx, request)
		if e != nil || !reflect.DeepEqual(found, again) {
			t.Fatalf("unstable: %v %v", again, e)
		}
	}
	strict, err := memory.NewGateway(store, memory.Access{ReadOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{MinRelevance: .6})
	if err != nil {
		t.Fatal(err)
	}
	found, err = strict.Search(ctx, request)
	if err != nil || len(found) != 1 || found[0].ID != records[0].ID {
		t.Fatalf("threshold: %v %v", found, err)
	}
}

func TestConfiguredStructuredDiscoveryIsReadOnlyAndReportsActualOperations(t *testing.T) {
	root := t.TempDir()
	configuration := memory.ProviderConfig{Version: "v1", ID: "local", Provider: memory.StructuredLocalProviderID, Configuration: memory.ConfigReference{Kind: "file", Name: root}, Capabilities: []memory.Capability{memory.CapabilitySearch}, Scopes: []memory.Scope{memory.ScopeProject}}
	status, err := memory.DiscoverConfiguredProvider(context.Background(), configuration)
	if err != nil {
		t.Fatal(err)
	}
	report := memory.BuildStatus(&configuration, status, nil, nil)
	if !report.Available || len(report.RequestedCapabilities) != 1 || !report.StructuredCapabilities.ConditionalUpdate || !report.StructuredCapabilities.AtomicSupersede || report.Ranking != "lexical-token-coverage" {
		t.Fatalf("discovery: %+v", report)
	}
	files, err := os.ReadDir(root)
	if err != nil || len(files) != 0 {
		t.Fatalf("discovery created state: %v %v", files, err)
	}
}

func TestProjectRegistryRejectsUnfaithfulIdentityAndReservedStateAliases(t *testing.T) {
	for _, name := range []string{"memory.json", "memory.lock", "MEMORY.JSON", "MEMORY.LOCK"} {
		if _, err := memory.OpenProjectRegistry(t.TempDir() + "/" + name); err == nil {
			t.Fatalf("reserved registry path accepted: %s", name)
		}
	}
	for _, data := range []string{`[{"id":"project-\ud800","directory":"/tmp/repo"}]`, "[{\"id\":\"project-\xff\",\"directory\":\"/tmp/repo\"}]"} {
		path := t.TempDir() + "/projects.json"
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := memory.OpenProjectRegistry(path); !errors.Is(err, memory.ErrUnavailable) {
			t.Fatalf("normalized trusted scalar: %q %v", data, err)
		}
	}
}

func TestProjectMappingConfirmationBindsDisplayedDirectoryIdentity(t *testing.T) {
	ctx := context.Background()
	registry, err := memory.OpenProjectRegistry(t.TempDir() + "/projects.json")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	path := root + "/project"
	if err = os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	preview, err := registry.PreviewRegister(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Plan().Directory != canonical || preview.Plan().Operation != "register" {
		t.Fatal(preview.Plan())
	}
	if err = os.Rename(path, path+"-moved"); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = registry.CommitMapping(ctx, preview, true); !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("replaced displayed directory accepted: %v", err)
	}
	if _, err = registry.Lookup(path); !errors.Is(err, memory.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestRegistryCannotAliasCanonicalStorageOrPersistentLock(t *testing.T) {
	root := t.TempDir()
	registryRoot := t.TempDir()
	provider, err := memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: "alias"}
	if _, err = memory.Remember(context.Background(), provider, memory.NewRecord{Owner: owner, Type: memory.TypeFact, Content: "canonical"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"memory.json", "memory.lock"} {
		path := registryRoot + "/" + name + "-alias.json"
		if err = os.Link(root+"/"+name, path); err != nil {
			t.Fatal(err)
		}
		if _, err = memory.OpenProjectRegistry(path, root); !errors.Is(err, memory.ErrInvalidInput) {
			t.Fatalf("canonical state alias accepted: %s %v", name, err)
		}
	}
}

func TestFreshGatewayConfirmedRetryReconcilesPersistedUncertainLifecycleReceipt(t *testing.T) {
	for _, operation := range []memory.Operation{memory.OperationUpdate, memory.OperationSupersede, memory.OperationForget} {
		t.Run(string(operation), func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			store, err := memory.OpenStructuredStore(root)
			if err != nil {
				t.Fatal(err)
			}
			owner := memory.Owner{Kind: memory.OwnerUser, UserID: "operator"}
			original, err := memory.Remember(ctx, store, memory.NewRecord{Owner: owner, Type: memory.TypeFact, Content: "v1", Source: "design"})
			if err != nil {
				t.Fatal(err)
			}
			uncertain, err := memory.OpenStructuredStore(root, memory.WithPersistence(memory.AtomicFilePersistence{Syncer: failingFileSync{directory: true}}))
			if err != nil {
				t.Fatal(err)
			}
			access := memory.Access{ReadOwners: []memory.Owner{owner}, WriteOwners: []memory.Owner{owner}}
			gateway, err := memory.NewGateway(uncertain, access, memory.RetrievalPolicy{})
			if err != nil {
				t.Fatal(err)
			}
			intent := memory.Mutation{Operation: operation, ID: original.ID, ExpectedVersion: 1, OperationID: "uncertain-" + string(operation), Record: memory.NewRecord{Owner: owner, Type: memory.TypeFact, Content: "v2", Source: "design"}}
			preview, err := gateway.Preview(ctx, intent)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = gateway.Commit(ctx, preview, memory.Confirmation{Confirmed: true, IntentID: preview.IntentID(), Owner: owner}); !errors.Is(err, memory.ErrOutcomeUnknown) {
				t.Fatal(err)
			}
			reopened, err := memory.OpenStructuredStore(root)
			if err != nil {
				t.Fatal(err)
			}
			fresh, err := memory.NewGateway(reopened, access, memory.RetrievalPolicy{})
			if err != nil {
				t.Fatal(err)
			}
			retry, err := fresh.Preview(ctx, intent)
			if err != nil {
				t.Fatalf("fresh preview blocked safe receipt reconciliation: %v", err)
			}
			if _, err = fresh.Commit(ctx, retry, memory.Confirmation{}); !errors.Is(err, memory.ErrNotConfirmed) {
				t.Fatal(err)
			}
			result, err := fresh.Commit(ctx, retry, memory.Confirmation{Confirmed: true, IntentID: retry.IntentID(), Owner: owner})
			if err != nil || len(result) != 1 {
				t.Fatal(result, err)
			}
			history, err := fresh.History(ctx, owner, original.ID)
			if err != nil || len(history) != 2 {
				t.Fatalf("retry repeated mutation: %v %v", history, err)
			}
			intent.OperationID = "new-stale-" + string(operation)
			stale, err := fresh.Preview(ctx, intent)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = fresh.Commit(ctx, stale, memory.Confirmation{Confirmed: true, IntentID: stale.IntentID(), Owner: owner}); !errors.Is(err, memory.ErrConflict) {
				t.Fatalf("new stale mutation bypassed CAS: %v", err)
			}
		})
	}
}
