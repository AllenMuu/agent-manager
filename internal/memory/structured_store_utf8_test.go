//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package memory_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/memory/memorytest"
)

func TestStructuredStoreRejectsNonUTF8RecordMetadata(t *testing.T) {
	ctx := context.Background()
	s, err := memory.OpenStructuredStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string]memory.NewRecord{}
	content := storeInput("a\xffb")
	inputs["content"] = content
	source := storeInput("good")
	source.Source = "source\xff"
	inputs["source"] = source
	evidence := storeInput("good")
	evidence.Evidence = []string{"good", "evidence\xff"}
	inputs["evidence"] = evidence
	for name, input := range inputs {
		t.Run(name, func(t *testing.T) {
			_, err := s.RememberWithOperation(ctx, memory.RememberRequest{Record: input, OperationID: "metadata-" + name})
			if !errors.Is(err, memory.ErrInvalidInput) {
				t.Fatalf("invalid %s accepted: %v", name, err)
			}
		})
	}
	if _, err = s.RememberWithOperation(ctx, memory.RememberRequest{Record: storeInput("a\xfeb"), OperationID: "metadata-content"}); !errors.Is(err, memory.ErrInvalidInput) {
		t.Fatalf("different invalid intent accepted: %v", err)
	}
	records, err := s.Recall(ctx, memory.Query{Owner: storeOwner})
	if err != nil || len(records) != 0 {
		t.Fatalf("invalid metadata persisted: %#v %v", records, err)
	}
}

func TestStructuredStoreRejectsNonUTF8Tokens(t *testing.T) {
	ctx := context.Background()
	s, err := memory.OpenStructuredStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	owners := []memory.Owner{{Kind: memory.OwnerProject, ProjectID: "p\xff"}, {Kind: memory.OwnerUser, UserID: "u\xff"}, {Kind: memory.OwnerAgent, ProjectID: "project", AgentID: "a\xff"}, {Kind: memory.OwnerSession, UserID: "user", SessionID: "s\xff"}}
	for _, owner := range owners {
		input := storeInput("good")
		input.Owner = owner
		if _, err = s.Remember(ctx, input); !errors.Is(err, memory.ErrInvalidInput) {
			t.Errorf("owner %#v accepted: %v", owner, err)
		}
	}
	if _, err = s.RememberWithOperation(ctx, memory.RememberRequest{Record: storeInput("good"), OperationID: "operation\xff"}); !errors.Is(err, memory.ErrInvalidInput) {
		t.Errorf("operation ID accepted: %v", err)
	}
	if _, err = s.Get(ctx, storeOwner, "record\xff"); !errors.Is(err, memory.ErrInvalidInput) {
		t.Errorf("Get ID accepted: %v", err)
	}
	if _, err = s.Update(ctx, memory.UpdateRequest{Owner: storeOwner, ID: "record\xff", ExpectedVersion: 1, Record: storeInput("good")}); !errors.Is(err, memory.ErrInvalidInput) {
		t.Errorf("Update ID accepted: %v", err)
	}
	if _, err = s.Forget(ctx, memory.MutationRequest{Owner: storeOwner, ID: "record\xff", ExpectedVersion: 1}); !errors.Is(err, memory.ErrInvalidInput) {
		t.Errorf("Forget ID accepted: %v", err)
	}
	normalized := memory.Owner{Kind: memory.OwnerProject, ProjectID: "p�"}
	records, err := s.Recall(ctx, memory.Query{Owner: normalized})
	if err != nil || len(records) != 0 {
		t.Fatalf("different owner received normalized records: %#v %v", records, err)
	}
}

func TestStructuredStoreInvalidOwnerCannotReplayReceipt(t *testing.T) {
	ctx := context.Background()
	s, err := memory.OpenStructuredStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	owner := memory.Owner{Kind: memory.OwnerProject, ProjectID: "p�"}
	input := storeInput("valid")
	input.Owner = owner
	record, err := s.Remember(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	request := memory.MutationRequest{Owner: owner, ID: record.ID, ExpectedVersion: 1, OperationID: "receipt"}
	if _, err = s.Forget(ctx, request); err != nil {
		t.Fatal(err)
	}
	request.Owner.ProjectID = "p\xff"
	if _, err = s.Forget(ctx, request); !errors.Is(err, memory.ErrInvalidInput) {
		t.Fatalf("normalized foreign owner replayed receipt: %v", err)
	}
}

func TestStructuredStoreRejectsNonUTF8LegacyImport(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "legacy.txt")
	raw := []byte("valid first line\na\xffb\n")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	request := memory.LegacyImportRequest{Path: path, Owner: storeOwner, Source: "declared", Type: memory.TypeFact, OperationID: "import-utf8"}
	confirmation := memory.ImportConfirmation{Confirmed: true, Owner: storeOwner, Source: "declared"}
	if _, err = s.ImportLegacy(ctx, request, confirmation); !errors.Is(err, memory.ErrInvalidInput) {
		t.Fatalf("invalid raw import accepted: %v", err)
	}
	records, err := s.Recall(ctx, memory.Query{Owner: storeOwner})
	if err != nil || len(records) != 0 {
		t.Fatalf("partial/normalized import %#v %v", records, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(raw, after) {
		t.Fatalf("legacy changed %q %v", after, err)
	}
	// Filesystems may reject byte-invalid names themselves. The import proposal
	// still must reject this path before it becomes an operation fingerprint.
	request.Path = filepath.Join(root, "legacy\xff.txt")
	_ = os.WriteFile(request.Path, []byte("valid line\n"), 0600)
	if _, err = s.ImportLegacy(ctx, request, confirmation); !errors.Is(err, memory.ErrInvalidInput) {
		t.Fatalf("invalid intent path accepted: %v", err)
	}
}

func TestStructuredStoreRejectsNonUTF8PersistedState(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s, err := memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	record, err := s.Remember(ctx, storeInput("trusted"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "memory.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	corrupted := bytes.ReplaceAll(data, []byte("trusted"), []byte("t\xffsted"))
	if err = os.WriteFile(path, corrupted, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, storeOwner, record.ID); !errors.Is(err, memory.ErrUnavailable) {
		t.Errorf("corrupt state normalized by Get: %v", err)
	}
	if _, err = memory.OpenStructuredStore(root); !errors.Is(err, memory.ErrUnavailable) {
		t.Errorf("corrupt state normalized on reopen: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(corrupted, after) {
		t.Fatalf("corrupt state implicitly rewritten %v", err)
	}
}

func TestStructuredStorePortableContract(t *testing.T) {
	memorytest.Run(t, func(t *testing.T) memory.StructuredProvider {
		s, err := memory.OpenStructuredStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		return s
	})
}

func TestStructuredStoreValidUnicodeRoundTrip(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s, err := memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	owner := memory.Owner{Kind: memory.OwnerUser, UserID: "用户甲"}
	input := memory.NewRecord{Owner: owner, Type: memory.TypeFact, Content: "可靠知识 🐼", Source: "来源：声明", Evidence: []string{"证据甲", "证据乙"}}
	request := memory.RememberRequest{Record: input, OperationID: "操作-一"}
	record, err := s.RememberWithOperation(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Get(ctx, owner, record.ID)
	if err != nil || !reflect.DeepEqual(got, record) {
		t.Fatalf("Unicode roundtrip %#v %v", got, err)
	}
	retry, err := reopened.RememberWithOperation(ctx, request)
	if err != nil || !reflect.DeepEqual(retry, record) {
		t.Fatalf("Unicode retry %#v %v", retry, err)
	}
}

func TestStructuredStoreInvalidMetadataMutationsLeaveCurrentRecord(t *testing.T) {
	ctx := context.Background()
	s, err := memory.OpenStructuredStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	record, err := s.Remember(ctx, storeInput("valid"))
	if err != nil {
		t.Fatal(err)
	}
	request := memory.UpdateRequest{Owner: storeOwner, ID: record.ID, ExpectedVersion: 1, Record: storeInput("invalid\xff"), OperationID: "mutation"}
	if _, err = s.Update(ctx, request); !errors.Is(err, memory.ErrInvalidInput) {
		t.Errorf("invalid update accepted: %v", err)
	}
	if _, err = s.Supersede(ctx, request); !errors.Is(err, memory.ErrInvalidInput) {
		t.Errorf("invalid supersede accepted: %v", err)
	}
	got, err := s.Get(ctx, storeOwner, record.ID)
	if err != nil || !reflect.DeepEqual(got, record) {
		t.Fatalf("invalid mutation altered original %#v %v", got, err)
	}
	request.Record = storeInput("valid change")
	updated, err := s.Update(ctx, request)
	if err != nil || updated.Version != 2 {
		t.Fatalf("rejected mutation retained receipt %#v %v", updated, err)
	}
}
