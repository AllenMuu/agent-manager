//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package memory_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/memory"
)

var storeOwner = memory.Owner{Kind: memory.OwnerProject, ProjectID: "project-one"}

func storeInput(content string) memory.NewRecord {
	return memory.NewRecord{Owner: storeOwner, Type: memory.TypeFact, Content: content, Source: "declared-source", Evidence: []string{"evidence-one"}, Layer: memory.LayerAtomic}
}
func TestStructuredStoreReopenOwnedRecord(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	store, err := memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	record, err := memory.Remember(ctx, store, storeInput("durable knowledge"))
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := memory.Get(ctx, reopened, storeOwner, record.ID)
	if err != nil || !reflect.DeepEqual(got, record) {
		t.Fatalf("reopen: %#v %v", got, err)
	}
	if record.Version != 1 || record.State != memory.RecordActive || record.ID == "" {
		t.Fatalf("canonical: %#v", record)
	}
	_, err = memory.Get(ctx, reopened, memory.Owner{Kind: memory.OwnerProject, ProjectID: "other"}, record.ID)
	if !errors.Is(err, memory.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestStructuredStoreCompetingProcesses(t *testing.T) {
	if root := os.Getenv("MEMORY_SUBPROCESS_ROOT"); root != "" {
		_, _ = io.ReadFull(os.Stdin, make([]byte, 1))
		s, err := memory.OpenStructuredStore(root)
		if err != nil {
			t.Fatal(err)
		}
		r, err := s.Update(context.Background(), memory.UpdateRequest{Owner: storeOwner, ID: memory.RecordID(os.Getenv("MEMORY_SUBPROCESS_ID")), ExpectedVersion: 1, Record: storeInput("changed")})
		if errors.Is(err, memory.ErrConflict) {
			fmt.Println("conflict")
			return
		}
		if err != nil || r.Version != 2 {
			t.Fatalf("update: %#v %v", r, err)
		}
		fmt.Println("committed")
		return
	}
	root := t.TempDir()
	s, err := memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Remember(context.Background(), storeInput("original"))
	if err != nil {
		t.Fatal(err)
	}
	unrelated, err := s.Remember(context.Background(), storeInput("unrelated"))
	if err != nil {
		t.Fatal(err)
	}
	commands := make([]*exec.Cmd, 2)
	inputs := make([]io.WriteCloser, 2)
	outputs := make([]bytes.Buffer, 2)
	for i := range commands {
		c := exec.Command(os.Args[0], "-test.run=^TestStructuredStoreCompetingProcesses$")
		c.Env = append(os.Environ(), "MEMORY_SUBPROCESS_ROOT="+root, "MEMORY_SUBPROCESS_ID="+string(r.ID))
		c.Stdout = &outputs[i]
		c.Stderr = &outputs[i]
		inputs[i], err = c.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err = c.Start(); err != nil {
			t.Fatal(err)
		}
		commands[i] = c
	}
	for _, input := range inputs {
		_, _ = input.Write([]byte("x"))
		input.Close()
	}
	commits, conflicts := 0, 0
	for i, c := range commands {
		if err = c.Wait(); err != nil {
			t.Fatalf("child: %v %s", err, outputs[i].String())
		}
		if strings.Contains(outputs[i].String(), "committed") {
			commits++
		}
		if strings.Contains(outputs[i].String(), "conflict") {
			conflicts++
		}
	}
	if commits != 1 || conflicts != 1 {
		t.Fatalf("commits=%d conflicts=%d", commits, conflicts)
	}
	got, err := s.Get(context.Background(), storeOwner, unrelated.ID)
	if err != nil || got.Content != "unrelated" {
		t.Fatalf("unrelated: %#v %v", got, err)
	}
}

func TestStructuredStoreSupersedeForgetHistory(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s, err := memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	original, err := s.Remember(ctx, storeInput("old"))
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := s.Supersede(ctx, memory.UpdateRequest{Owner: storeOwner, ID: original.ID, ExpectedVersion: 1, Record: storeInput("replacement")})
	if err != nil {
		t.Fatal(err)
	}
	if replacement.ID == original.ID || replacement.Supersedes != original.ID || replacement.Version != 1 {
		t.Fatalf("replacement %#v", replacement)
	}
	_, err = s.Forget(ctx, memory.MutationRequest{Owner: storeOwner, ID: replacement.ID, ExpectedVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	s, err = memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	active, err := s.Recall(ctx, memory.Query{Owner: storeOwner})
	if err != nil || len(active) != 0 {
		t.Fatalf("recall %#v %v", active, err)
	}
	oldHistory, err := s.History(ctx, storeOwner, original.ID)
	if err != nil || len(oldHistory) != 2 || oldHistory[0].State != memory.RecordActive || oldHistory[1].State != memory.RecordSuperseded || oldHistory[1].SupersededBy != replacement.ID {
		t.Fatalf("old history %#v %v", oldHistory, err)
	}
	newHistory, err := s.History(ctx, storeOwner, replacement.ID)
	if err != nil || len(newHistory) != 2 || newHistory[1].State != memory.RecordDeleted || newHistory[1].Supersedes != original.ID {
		t.Fatalf("new history %#v %v", newHistory, err)
	}
	_, err = s.History(ctx, memory.Owner{Kind: memory.OwnerProject, ProjectID: "other"}, original.ID)
	if !errors.Is(err, memory.ErrNotFound) {
		t.Fatal(err)
	}
}

type failingFileSync struct{ directory bool }

func (f failingFileSync) Sync(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.IsDir() == f.directory {
		return errors.New("injected filesystem sync failure")
	}
	return file.Sync()
}
func TestStructuredStoreSupersessionFailsBeforeCommit(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s, err := memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	old, err := s.Remember(ctx, storeInput("old"))
	if err != nil {
		t.Fatal(err)
	}
	failing, err := memory.OpenStructuredStore(root, memory.WithPersistence(memory.AtomicFilePersistence{Syncer: failingFileSync{}}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = failing.Supersede(ctx, memory.UpdateRequest{Owner: storeOwner, ID: old.ID, ExpectedVersion: 1, Record: storeInput("replacement")})
	if !errors.Is(err, memory.ErrNotCommitted) || errors.Is(err, memory.ErrOutcomeUnknown) {
		t.Fatalf("expected known precommit failure: %v", err)
	}
	s, err = memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	active, err := s.Recall(ctx, memory.Query{Owner: storeOwner})
	if err != nil || len(active) != 1 || !reflect.DeepEqual(active[0], old) {
		t.Fatalf("old active %#v %v", active, err)
	}
	history, err := s.History(ctx, storeOwner, old.ID)
	if err != nil || len(history) != 1 {
		t.Fatalf("history %#v %v", history, err)
	}
}

func TestStructuredStoreFailureRetryEvidence(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s, err := memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	input := memory.RememberRequest{Record: storeInput("original"), OperationID: "create-one"}
	old, err := s.RememberWithOperation(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	uncertain, err := memory.OpenStructuredStore(root, memory.WithPersistence(memory.AtomicFilePersistence{Syncer: failingFileSync{directory: true}}))
	if err != nil {
		t.Fatal(err)
	}
	update := memory.UpdateRequest{Owner: storeOwner, ID: old.ID, ExpectedVersion: 1, Record: storeInput("changed"), OperationID: "update-one"}
	_, err = uncertain.Update(ctx, update)
	if !errors.Is(err, memory.ErrOutcomeUnknown) {
		t.Fatalf("outcome: %v", err)
	}
	s, err = memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Update(ctx, update)
	if err != nil || result.Version != 2 || result.Content != "changed" {
		t.Fatalf("retry %#v %v", result, err)
	}
	repeated, err := s.Update(ctx, update)
	if err != nil || !reflect.DeepEqual(repeated, result) {
		t.Fatalf("repeat %#v %v", repeated, err)
	}
	history, err := s.History(ctx, storeOwner, old.ID)
	if err != nil || len(history) != 2 {
		t.Fatalf("history %#v %v", history, err)
	}
	update.Record.Content = "different intent"
	if _, err = s.Update(ctx, update); !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("intent: %v", err)
	}
	update.Record = storeInput("changed")
	update.Owner = memory.Owner{Kind: memory.OwnerProject, ProjectID: "other"}
	update.Record.Owner = update.Owner
	if _, err = s.Update(ctx, update); !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("owner intent: %v", err)
	}
	created, err := s.RememberWithOperation(ctx, input)
	if err != nil || !reflect.DeepEqual(created, old) {
		t.Fatalf("create retry %#v %v", created, err)
	}
}

func TestStructuredStoreNoImplicitConversion(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "legacy.txt")
	legacy := []byte("old text\n")
	if err := os.WriteFile(path, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		s, err := memory.OpenStructuredStore(root)
		if err != nil {
			t.Fatal(err)
		}
		health, err := s.Health(context.Background())
		if err != nil || !health.Available {
			t.Fatalf("health %#v %v", health, err)
		}
		records, err := s.Recall(context.Background(), memory.Query{Owner: storeOwner})
		if err != nil || len(records) != 0 {
			t.Fatalf("records %#v %v", records, err)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, legacy) {
		t.Fatalf("legacy changed %q %v", after, err)
	}
	if _, err := memory.OpenStructuredStore(path); err == nil {
		t.Fatal("legacy text accepted as root")
	}
}
func TestStructuredStoreConfirmedLegacyImport(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "legacy.txt")
	legacy := []byte("first line\n\nsecond line\n")
	if err := os.WriteFile(path, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	request := memory.LegacyImportRequest{Path: path, Owner: storeOwner, Source: "operator-declared", Type: memory.TypeTask, OperationID: "import-one"}
	if _, err = s.ImportLegacy(context.Background(), request, memory.ImportConfirmation{}); !errors.Is(err, memory.ErrInvalidInput) {
		t.Fatalf("unconfirmed: %v", err)
	}
	confirmation := memory.ImportConfirmation{Confirmed: true, Owner: storeOwner, Source: "operator-declared"}
	records, err := s.ImportLegacy(context.Background(), request, confirmation)
	if err != nil || len(records) != 2 {
		t.Fatalf("import %#v %v", records, err)
	}
	for _, r := range records {
		if r.Source != "operator-declared" || len(r.Evidence) != 0 || r.Owner != storeOwner || r.Type != memory.TypeTask {
			t.Fatalf("metadata %#v", r)
		}
	}
	reopened, err := memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := reopened.ImportLegacy(context.Background(), request, confirmation)
	if err != nil || !reflect.DeepEqual(retry, records) {
		t.Fatalf("retry %#v %v", retry, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, legacy) {
		t.Fatalf("legacy changed %q %v", after, err)
	}
}

func TestStructuredStoreRejectsForeignFormat(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "memory.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.OpenStructuredStore(root); !errors.Is(err, memory.ErrUnavailable) {
		t.Fatalf("foreign format: %v", err)
	}
}

func TestStructuredStoreCompetingInstances(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	a, err := memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	b, err := memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	record, err := a.Remember(ctx, storeInput("initial"))
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, store := range []*memory.StructuredStore{a, b} {
		go func(s *memory.StructuredStore) {
			<-start
			result, e := s.Update(ctx, memory.UpdateRequest{Owner: storeOwner, ID: record.ID, ExpectedVersion: 1, Record: storeInput("updated")})
			if e == nil && result.Version != 2 {
				e = fmt.Errorf("version=%d", result.Version)
			}
			results <- e
		}(store)
	}
	close(start)
	commits, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		e := <-results
		if e == nil {
			commits++
		} else if errors.Is(e, memory.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if commits != 1 || conflicts != 1 {
		t.Fatalf("commits=%d conflicts=%d", commits, conflicts)
	}
}

type waitingPersistence struct{}

func (waitingPersistence) Commit(ctx context.Context, dir *os.File, data []byte) error {
	fmt.Println("LOCK HELD")
	_, err := io.ReadFull(os.Stdin, make([]byte, 1))
	if err != nil {
		return err
	}
	return (memory.AtomicFilePersistence{}).Commit(ctx, dir, data)
}
func TestStructuredStoreCancellableProcessLock(t *testing.T) {
	if root := os.Getenv("MEMORY_LOCK_CHILD"); root != "" {
		s, err := memory.OpenStructuredStore(root, memory.WithPersistence(waitingPersistence{}))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.Remember(context.Background(), storeInput("holder")); err != nil {
			t.Fatal(err)
		}
		return
	}
	root := t.TempDir()
	s, err := memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	record, err := s.Remember(context.Background(), storeInput("initial"))
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestStructuredStoreCancellableProcessLock$")
	child.Env = append(os.Environ(), "MEMORY_LOCK_CHILD="+root)
	input, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	child.Stderr = &stderr
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = input.Close(); _ = child.Process.Kill(); _ = child.Wait() }()
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(output).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		if line != "LOCK HELD\n" {
			t.Fatalf("child: %q %s", line, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("holder not ready")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = s.Update(ctx, memory.UpdateRequest{Owner: storeOwner, ID: record.ID, ExpectedVersion: 1, Record: storeInput("waiting")})
	if !errors.Is(err, memory.ErrCanceled) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation %v", err)
	}
	_, _ = input.Write([]byte("x"))
	input.Close()
	if err = child.Wait(); err != nil {
		t.Fatalf("child: %v %s", err, stderr.String())
	}
	got, err := s.Get(context.Background(), storeOwner, record.ID)
	if err != nil || got.Version != 1 {
		t.Fatalf("canceled update %#v %v", got, err)
	}
}

func TestStructuredStoreLifecycleVersionsAndRetries(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	s, err := memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	old, err := s.Remember(ctx, storeInput("old"))
	if err != nil {
		t.Fatal(err)
	}
	request := memory.UpdateRequest{Owner: storeOwner, ID: old.ID, ExpectedVersion: 1, Record: storeInput("next"), OperationID: "supersede"}
	replacement, err := s.Supersede(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.Supersede(ctx, request)
	if err != nil || !reflect.DeepEqual(retry, replacement) {
		t.Fatalf("supersede retry %#v %v", retry, err)
	}
	_, err = s.Forget(ctx, memory.MutationRequest{Owner: storeOwner, ID: replacement.ID, ExpectedVersion: 0})
	if !errors.Is(err, memory.ErrInvalidInput) {
		t.Fatalf("zero version: %v", err)
	}
	updated, err := s.Update(ctx, memory.UpdateRequest{Owner: storeOwner, ID: replacement.ID, ExpectedVersion: 1, Record: storeInput("changed"), OperationID: "update-next"})
	if err != nil || updated.Supersedes != old.ID || updated.Version != 2 {
		t.Fatalf("update %#v %v", updated, err)
	}
	_, err = s.Forget(ctx, memory.MutationRequest{Owner: storeOwner, ID: replacement.ID, ExpectedVersion: 1})
	if !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("stale forget %v", err)
	}
	_, err = s.Supersede(ctx, memory.UpdateRequest{Owner: storeOwner, ID: replacement.ID, ExpectedVersion: 1, Record: storeInput("newer")})
	if !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("stale supersede %v", err)
	}
	forget := memory.MutationRequest{Owner: storeOwner, ID: replacement.ID, ExpectedVersion: 2, OperationID: "forget"}
	deleted, err := s.Forget(ctx, forget)
	if err != nil || deleted.State != memory.RecordDeleted || deleted.Version != 3 {
		t.Fatalf("deleted %#v %v", deleted, err)
	}
	reopened, err := memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	retry, err = reopened.Forget(ctx, forget)
	if err != nil || !reflect.DeepEqual(retry, deleted) {
		t.Fatalf("forget retry %#v %v", retry, err)
	}
	_, err = reopened.Update(ctx, memory.UpdateRequest{Owner: storeOwner, ID: replacement.ID, ExpectedVersion: 3, Record: storeInput("resurrect")})
	if !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("retired update %v", err)
	}
	history, err := reopened.History(ctx, storeOwner, replacement.ID)
	if err != nil || len(history) != 3 {
		t.Fatalf("history %#v %v", history, err)
	}
}

func TestStructuredStoreGuardsFileAndDirectoryIdentity(t *testing.T) {
	ctx := context.Background()
	t.Run("direct root", func(t *testing.T) {
		root := t.TempDir()
		link := filepath.Join(t.TempDir(), "link")
		if err := os.Symlink(root, link); err != nil {
			t.Fatal(err)
		}
		if _, err := memory.OpenStructuredStore(link); !errors.Is(err, memory.ErrUnavailable) {
			t.Fatalf("symlink root %v", err)
		}
	})
	for _, name := range []string{"memory.json", "memory.lock"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			s, err := memory.OpenStructuredStore(root)
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(t.TempDir(), "target")
			if err = os.WriteFile(target, []byte("untouched"), 0600); err != nil {
				t.Fatal(err)
			}
			if err = os.Symlink(target, filepath.Join(root, name)); err != nil {
				t.Fatal(err)
			}
			if _, err = s.Remember(ctx, storeInput("blocked")); err == nil {
				t.Fatal("symlink accepted")
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "untouched" {
				t.Fatalf("target %q %v", data, err)
			}
		})
	}
	t.Run("replaced directory", func(t *testing.T) {
		parent := t.TempDir()
		root := filepath.Join(parent, "store")
		if err := os.Mkdir(root, 0700); err != nil {
			t.Fatal(err)
		}
		s, err := memory.OpenStructuredStore(root)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.Rename(root, filepath.Join(parent, "old")); err != nil {
			t.Fatal(err)
		}
		if err = os.Mkdir(root, 0700); err != nil {
			t.Fatal(err)
		}
		if _, err = s.Remember(ctx, storeInput("blocked")); !errors.Is(err, memory.ErrUnavailable) {
			t.Fatalf("identity %v", err)
		}
		replacement, err := memory.OpenStructuredStore(root)
		if err != nil {
			t.Fatal(err)
		}
		records, err := replacement.Recall(ctx, memory.Query{Owner: storeOwner})
		if err != nil || len(records) != 0 {
			t.Fatalf("replacement %#v %v", records, err)
		}
	})
}

func TestStructuredStoreLegacyImportAtomicFailure(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "legacy.txt")
	if err := os.WriteFile(path, []byte("first\nsecond\n"), 0600); err != nil {
		t.Fatal(err)
	}
	request := memory.LegacyImportRequest{Path: path, Owner: storeOwner, Source: "declared", Type: memory.TypeSkill, OperationID: "batch"}
	confirmation := memory.ImportConfirmation{Confirmed: true, Owner: storeOwner, Source: "declared"}
	s, err := memory.OpenStructuredStore(root, memory.WithPersistence(memory.AtomicFilePersistence{Syncer: failingFileSync{}}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ImportLegacy(context.Background(), request, confirmation); err == nil || errors.Is(err, memory.ErrOutcomeUnknown) {
		t.Fatalf("failure %v", err)
	}
	healthy, err := memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	records, err := healthy.Recall(context.Background(), memory.Query{Owner: storeOwner})
	if err != nil || len(records) != 0 {
		t.Fatalf("partial batch %#v %v", records, err)
	}
	records, err = healthy.ImportLegacy(context.Background(), request, confirmation)
	if err != nil || len(records) != 2 {
		t.Fatalf("retry %#v %v", records, err)
	}
	confirmation.Source = "other"
	if _, err = healthy.ImportLegacy(context.Background(), request, confirmation); !errors.Is(err, memory.ErrInvalidInput) {
		t.Fatalf("source confirmation %v", err)
	}
	confirmation.Source = "declared"
	request.Source = ""
	confirmation.Source = ""
	if _, err = healthy.ImportLegacy(context.Background(), request, confirmation); !errors.Is(err, memory.ErrInvalidInput) {
		t.Fatalf("missing source %v", err)
	}
}

func TestStructuredStoreRetryMustConfirmDurability(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	healthy, err := memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	old, err := healthy.Remember(ctx, storeInput("original"))
	if err != nil {
		t.Fatal(err)
	}
	uncertain, err := memory.OpenStructuredStore(root, memory.WithPersistence(memory.AtomicFilePersistence{Syncer: failingFileSync{directory: true}}))
	if err != nil {
		t.Fatal(err)
	}
	request := memory.UpdateRequest{Owner: storeOwner, ID: old.ID, ExpectedVersion: 1, Record: storeInput("changed"), OperationID: "uncertain"}
	if _, err = uncertain.Update(ctx, request); !errors.Is(err, memory.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	if _, err = uncertain.Update(ctx, request); !errors.Is(err, memory.ErrOutcomeUnknown) {
		t.Fatalf("retry falsely confirmed durability: %v", err)
	}
	result, err := healthy.Update(ctx, request)
	if err != nil || result.Version != 2 {
		t.Fatalf("confirmed %#v %v", result, err)
	}
}

func (waitingPersistence) Confirm(ctx context.Context, dir *os.File) error {
	return (memory.AtomicFilePersistence{}).Confirm(ctx, dir)
}
