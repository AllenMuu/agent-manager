//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package memory_test

import (
	"context"
	"errors"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
	plan := preview.Plan()
	plan.Record.Content = "swapped plan"
	plan.Record.Evidence[0] = "swapped plan"
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
			if operation == memory.OperationForget {
				intent.Record = memory.NewRecord{Owner: owner}
			}
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

func TestGatewayRejectsIneffectiveMutationInputBeforePreview(t *testing.T) {
	ctx := context.Background()
	store, err := memory.OpenStructuredStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	owner := memory.Owner{Kind: memory.OwnerUser, UserID: "operator"}
	original, err := memory.Remember(ctx, store, memory.NewRecord{Owner: owner, Type: memory.TypeFact, Content: "original", Source: "design"})
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := memory.NewGateway(store, memory.Access{ReadOwners: []memory.Owner{owner}, WriteOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "legacy.txt")
	if err := os.WriteFile(source, []byte("lesson\n"), 0600); err != nil {
		t.Fatal(err)
	}
	body := memory.NewRecord{Owner: owner, Type: memory.TypeFact, Content: "unused", Source: "unused", Evidence: []string{"unused"}, Layer: memory.LayerAtomic}
	imported := memory.LegacyImportRequest{Path: source, Owner: owner, Source: "declared", Type: memory.TypeExperience}
	tests := []struct {
		name   string
		intent memory.Mutation
	}{
		{"forget-body", memory.Mutation{Operation: memory.OperationForget, Record: body, ID: original.ID, ExpectedVersion: 1}},
		{"import-body", memory.Mutation{Operation: memory.OperationImport, Record: body, Import: imported}},
		{"add-target", memory.Mutation{Operation: memory.OperationAdd, Record: body, ID: original.ID, ExpectedVersion: 1}},
		{"update-import", memory.Mutation{Operation: memory.OperationUpdate, Record: body, ID: original.ID, ExpectedVersion: 1, Import: imported}},
		{"import-target", memory.Mutation{Operation: memory.OperationImport, Record: memory.NewRecord{Owner: owner}, Import: imported, ID: original.ID, ExpectedVersion: 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := gateway.Preview(ctx, test.intent); !errors.Is(err, memory.ErrInvalidInput) {
				t.Fatalf("ineffective input accepted: %v", err)
			}
		})
	}
	results, err := gateway.Search(ctx, memory.SearchRequest{Owner: owner})
	if err != nil || len(results) != 1 || results[0].Content != "original" || results[0].Version != 1 {
		t.Fatalf("invalid preview changed records: %v %v", results, err)
	}
}

func TestImportPreviewHonorsExplicitReceiptAndContentConstraints(t *testing.T) {
	ctx := context.Background()
	store, err := memory.OpenStructuredStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	owner := memory.Owner{Kind: memory.OwnerUser, UserID: "operator"}
	gateway, err := memory.NewGateway(store, memory.Access{ReadOwners: []memory.Owner{owner}, WriteOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "legacy.txt")
	if err := os.WriteFile(source, []byte("lesson\n"), 0600); err != nil {
		t.Fatal(err)
	}
	intent := memory.Mutation{Operation: memory.OperationImport, Record: memory.NewRecord{Owner: owner}, Import: memory.LegacyImportRequest{Path: source, Owner: owner, Source: "declared", Type: memory.TypeExperience, OperationID: "retained-import"}}
	preview, err := gateway.Preview(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	plan := preview.Plan()
	if plan.OperationID != "retained-import" || plan.Import.OperationID != plan.OperationID {
		t.Errorf("explicit receipt intent changed: %+v", plan)
	}
	intent.OperationID = "different-import"
	if _, err := gateway.Preview(ctx, intent); !errors.Is(err, memory.ErrInvalidInput) {
		t.Errorf("contradictory operation IDs: %v", err)
	}
	intent.OperationID = ""
	intent.Import.ExpectedContentSHA256 = strings.Repeat("0", 64)
	if _, err := gateway.Preview(ctx, intent); !errors.Is(err, memory.ErrConflict) {
		t.Errorf("explicit content constraint ignored: %v", err)
	}
	intent.Import.ExpectedContentSHA256 = ""
	intent.Import.Source = "  "
	if _, err := gateway.Preview(ctx, intent); !errors.Is(err, memory.ErrInvalidInput) {
		t.Errorf("blank source previewed: %v", err)
	}
	results, err := gateway.Search(ctx, memory.SearchRequest{Owner: owner})
	if err != nil || len(results) != 0 {
		t.Fatalf("preview mutated: %v %v", results, err)
	}
}

func TestRegistryFirstMutationChecksUnavailableProviderRoots(t *testing.T) {
	for _, replacement := range []string{"regular-file", "symlink"} {
		t.Run(replacement, func(t *testing.T) {
			providerRoot := filepath.Join(t.TempDir(), "store")
			if replacement == "regular-file" {
				if err := os.WriteFile(providerRoot, []byte("unavailable"), 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Symlink(t.TempDir(), providerRoot); err != nil {
				t.Fatal(err)
			}
			registryPath := filepath.Join(t.TempDir(), "projects.json")
			registry, err := memory.OpenProjectRegistry(registryPath, providerRoot)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := registry.Register(context.Background(), t.TempDir(), true); !errors.Is(err, memory.ErrUnavailable) {
				t.Fatalf("first mutation bypassed strict root checks: %v", err)
			}
			if _, err := os.Stat(registryPath); !os.IsNotExist(err) {
				t.Fatalf("failed mutation created registry: %v", err)
			}
		})
	}
}

func TestProjectRegistryRecognizesRealCaseInsensitiveDirectoryAlias(t *testing.T) {
	root := t.TempDir()
	original := filepath.Join(root, "CaseRepo")
	alias := filepath.Join(root, "caserepo")
	if err := os.Mkdir(original, 0700); err != nil {
		t.Fatal(err)
	}
	first, err := os.Stat(original)
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.Stat(alias)
	if os.IsNotExist(err) {
		t.Skip("temporary filesystem is case-sensitive; case-insensitive runtime not available")
	}
	if err != nil || !os.SameFile(first, second) {
		t.Fatalf("case alias identity: %v", err)
	}
	registry, err := memory.OpenProjectRegistry(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	registered, err := registry.Register(context.Background(), original, true)
	if err != nil {
		t.Fatal(err)
	}
	found, err := registry.Lookup(alias)
	if err != nil || found.ID != registered.ID {
		t.Fatalf("case alias lost registered identity: %v %v", found, err)
	}
	if _, err := registry.Register(context.Background(), alias, true); !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("case alias acquired second ID: %v", err)
	}
}

// operationOnlyLocalProvider exposes the operation-aware write seam over the
// real store without embedding its separate basic RecordWriter interface.
type operationOnlyLocalProvider struct {
	store    *memory.StructuredStore
	declared bool
}

func (p operationOnlyLocalProvider) Capabilities() memory.StructuredCapabilities {
	return memory.StructuredCapabilities{Remember: p.declared}
}
func (p operationOnlyLocalProvider) Health(ctx context.Context) (memory.HealthStatus, error) {
	return p.store.Health(ctx)
}
func (p operationOnlyLocalProvider) RememberWithOperation(ctx context.Context, request memory.RememberRequest) (memory.Record, error) {
	return p.store.RememberWithOperation(ctx, request)
}

func TestGatewayDiscoversOperationAwareOnlyConfirmedAdd(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	owner := memory.Owner{Kind: memory.OwnerUser, UserID: "operator"}
	provider := operationOnlyLocalProvider{store: store, declared: true}
	gateway, err := memory.NewGateway(provider, memory.Access{WriteOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	input := memory.NewRecord{Owner: owner, Type: memory.TypeFact, Content: "confirmed durable fact", Source: "report:F1"}
	if _, err := memory.Remember(ctx, provider, input); !errors.Is(err, memory.ErrUnsupported) {
		t.Fatalf("basic Remember acquired an operation-aware fallback: %v", err)
	}
	preview, err := gateway.Preview(ctx, memory.Mutation{Operation: memory.OperationAdd, Record: input, OperationID: "confirmed-add"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gateway.Commit(ctx, preview, memory.Confirmation{}); !errors.Is(err, memory.ErrNotConfirmed) {
		t.Fatalf("unconfirmed write: %v", err)
	}
	records, err := memory.Recall(ctx, store, memory.Query{Owner: owner})
	if err != nil || len(records) != 0 {
		t.Fatalf("unconfirmed add mutated: %v %v", records, err)
	}
	created, err := gateway.Commit(ctx, preview, memory.Confirmation{Confirmed: true, IntentID: preview.IntentID(), Owner: owner})
	if err != nil || len(created) != 1 {
		t.Fatalf("operation-aware confirmed add: %v %v", created, err)
	}
	reopened, err := memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := memory.Get(ctx, reopened, owner, created[0].ID)
	if err != nil || persisted.Content != input.Content {
		t.Fatalf("durable record: %v %v", persisted, err)
	}
	report := gateway.Status(ctx, &memory.ProviderConfig{Provider: "operation-aware-only", Capabilities: []memory.Capability{memory.CapabilityWrite}})
	if report.StructuredCapabilities == nil || !report.StructuredCapabilities.Remember || !reflect.DeepEqual(report.Capabilities, []memory.Capability{memory.CapabilityWrite}) || len(report.UnsupportedCapabilities) != 0 {
		t.Fatalf("implemented confirmed add reported unsupported: %+v", report)
	}
}

type declaredWriteOnlyLocalProvider struct{ store *memory.StructuredStore }

func (p declaredWriteOnlyLocalProvider) Capabilities() memory.StructuredCapabilities {
	return memory.StructuredCapabilities{Remember: true}
}
func (p declaredWriteOnlyLocalProvider) Health(ctx context.Context) (memory.HealthStatus, error) {
	return p.store.Health(ctx)
}

func TestGatewayConfirmedAddDiscoveryRequiresInterfaceAndDeclaration(t *testing.T) {
	for _, variant := range []string{"both-interfaces", "undeclared-operation-interface", "declaration-without-interface"} {
		t.Run(variant, func(t *testing.T) {
			ctx := context.Background()
			store, err := memory.OpenStructuredStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			var provider memory.StructuredProvider = store
			implemented := variant == "both-interfaces"
			switch variant {
			case "undeclared-operation-interface":
				provider = operationOnlyLocalProvider{store: store, declared: false}
			case "declaration-without-interface":
				provider = declaredWriteOnlyLocalProvider{store: store}
			}
			owner := memory.Owner{Kind: memory.OwnerUser, UserID: "operator"}
			gateway, err := memory.NewGateway(provider, memory.Access{WriteOwners: []memory.Owner{owner}}, memory.RetrievalPolicy{})
			if err != nil {
				t.Fatal(err)
			}
			report := gateway.Status(ctx, &memory.ProviderConfig{Provider: variant, Capabilities: []memory.Capability{memory.CapabilityWrite}})
			hasWrite := false
			for _, capability := range report.Capabilities {
				if capability == memory.CapabilityWrite {
					hasWrite = true
				}
			}
			if report.StructuredCapabilities == nil || report.StructuredCapabilities.Remember != implemented || hasWrite != implemented || (len(report.UnsupportedCapabilities) == 0) != implemented {
				t.Fatalf("interface/declaration status mismatch: %+v", report)
			}
			input := memory.NewRecord{Owner: owner, Type: memory.TypeFact, Content: "confirmed fact", Source: "report:F1"}
			preview, err := gateway.Preview(ctx, memory.Mutation{Operation: memory.OperationAdd, Record: input})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := gateway.Commit(ctx, preview, memory.Confirmation{}); !errors.Is(err, memory.ErrNotConfirmed) {
				t.Fatalf("unconfirmed write: %v", err)
			}
			records, err := memory.Recall(ctx, store, memory.Query{Owner: owner})
			if err != nil || len(records) != 0 {
				t.Fatalf("unconfirmed mutation: %v %v", records, err)
			}
			created, err := gateway.Commit(ctx, preview, memory.Confirmation{Confirmed: true, IntentID: preview.IntentID(), Owner: owner})
			if implemented {
				if err != nil || len(created) != 1 || created[0].Content != input.Content {
					t.Fatalf("both-interface store lost confirmed add: %v %v", created, err)
				}
				basic, err := memory.Remember(ctx, provider, input)
				if err != nil || basic.Content != input.Content {
					t.Fatalf("both-interface store lost basic Remember: %v %v", basic, err)
				}
			} else {
				if !errors.Is(err, memory.ErrUnsupported) {
					t.Fatalf("unsupported interface/declaration wrote: %v", err)
				}
				records, err := memory.Recall(ctx, store, memory.Query{Owner: owner})
				if err != nil || len(records) != 0 {
					t.Fatalf("unsupported add mutated: %v %v", records, err)
				}
			}
		})
	}
}
