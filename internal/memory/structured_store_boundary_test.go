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
)

func TestStructuredStoreRejectsImportFromCanonicalDestination(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s, err := memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	original, err := s.Remember(ctx, storeInput("original"))
	if err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(root, "memory.json")
	before, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err = os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	confirmation := memory.ImportConfirmation{Confirmed: true, Owner: storeOwner, Source: "declared"}
	request := memory.LegacyImportRequest{Owner: storeOwner, Source: "declared", Type: memory.TypeFact, OperationID: "import-isolation"}
	for _, path := range []string{canonical, filepath.Join(alias, "memory.json")} {
		request.Path = path
		for retry := 0; retry < 2; retry++ {
			if _, err = s.ImportLegacy(ctx, request, confirmation); !errors.Is(err, memory.ErrInvalidInput) {
				t.Errorf("canonical source %q accepted: %v", path, err)
			}
		}
		after, err := os.ReadFile(canonical)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("canonical source changed: %v", err)
		}
	}
	records, err := s.Recall(ctx, memory.Query{Owner: storeOwner})
	if err != nil || len(records) != 1 || !reflect.DeepEqual(records[0], original) {
		t.Fatalf("overlap created records %#v %v", records, err)
	}
	request.Path = filepath.Join(root, "legacy.txt")
	source := []byte("distinct legacy knowledge\n")
	if err = os.WriteFile(request.Path, source, 0600); err != nil {
		t.Fatal(err)
	}
	imported, err := s.ImportLegacy(ctx, request, confirmation)
	if err != nil || len(imported) != 1 {
		t.Fatalf("normal import/rejected receipt %#v %v", imported, err)
	}
	retry, err := s.ImportLegacy(ctx, request, confirmation)
	if err != nil || !reflect.DeepEqual(retry, imported) {
		t.Fatalf("normal retry %#v %v", retry, err)
	}
	after, err := os.ReadFile(request.Path)
	if err != nil || !bytes.Equal(source, after) {
		t.Fatalf("normal source changed %v", err)
	}
}

func TestStructuredStoreRejectsUnpairedJSONSurrogates(t *testing.T) {
	cases := []struct {
		name, old, replacement string
		owner                  memory.Owner
		idSuffix               string
	}{
		{name: "high content", old: "trusted", replacement: `trust\ud800ed`, owner: storeOwner},
		{name: "low content", old: "trusted", replacement: `trust\udc00ed`, owner: storeOwner},
		{name: "high followed by high", old: "trusted", replacement: `trust\ud800\ud801ed`, owner: storeOwner},
		{name: "owner", old: "project-one", replacement: `project-one\ud800`, owner: memory.Owner{Kind: memory.OwnerProject, ProjectID: "project-one�"}},
		{name: "record identity and object keys", replacement: `\udc00`, owner: storeOwner, idSuffix: "�"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
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
			old, replacement := tc.old, tc.replacement
			if tc.idSuffix != "" {
				old = string(record.ID)
				replacement = old + replacement
			}
			corrupted := bytes.ReplaceAll(data, []byte(old), []byte(replacement))
			if err = os.WriteFile(path, corrupted, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = s.Get(ctx, tc.owner, memory.RecordID(string(record.ID)+tc.idSuffix)); !errors.Is(err, memory.ErrUnavailable) {
				t.Errorf("malformed scalar exposed as trusted state: %v", err)
			}
			if _, err = memory.OpenStructuredStore(root); !errors.Is(err, memory.ErrUnavailable) {
				t.Errorf("malformed scalar accepted on reopen: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, corrupted) {
				t.Fatalf("corrupt bytes changed: %v", err)
			}
		})
	}
}

func TestStructuredStorePreservesValidJSONUnicodeEscapes(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s, err := memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	input := storeInput(`知识 🐼 � literal \ud800`)
	input.Source = "知识来源"
	input.Evidence = []string{"证据 🐼"}
	record, err := s.Remember(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "memory.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	escaped := bytes.ReplaceAll(data, []byte("🐼"), []byte(`\uD83D\uDC3C`))
	escaped = bytes.ReplaceAll(escaped, []byte("知识"), []byte(`\u77e5\u8bc6`))
	escaped = bytes.ReplaceAll(escaped, []byte("project-one"), []byte(`project-\u006fne`))
	if err = os.WriteFile(path, escaped, 0600); err != nil {
		t.Fatal(err)
	}
	reopened, err := memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Get(ctx, storeOwner, record.ID)
	if err != nil || !reflect.DeepEqual(got, record) {
		t.Fatalf("legitimate Unicode changed %#v %v", got, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, escaped) {
		t.Fatalf("valid escaped source rewritten %v", err)
	}
}

func TestStructuredStoreRejectsCanonicalDestinationFileAliases(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s, err := memory.OpenStructuredStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Remember(ctx, storeInput("original")); err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(root, "memory.json")
	before, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatal(err)
	}
	aliases := []string{}
	caseAlias := filepath.Join(root, "MEMORY.JSON")
	originalInfo, err := os.Stat(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if aliasInfo, e := os.Stat(caseAlias); e == nil && os.SameFile(originalInfo, aliasInfo) {
		aliases = append(aliases, caseAlias)
	}
	hardlink := filepath.Join(t.TempDir(), "legacy.txt")
	if err = os.Link(canonical, hardlink); err != nil {
		t.Fatal(err)
	}
	aliases = append(aliases, hardlink)
	confirmation := memory.ImportConfirmation{Confirmed: true, Owner: storeOwner, Source: "declared"}
	for _, path := range aliases {
		request := memory.LegacyImportRequest{Path: path, Owner: storeOwner, Source: "declared", Type: memory.TypeFact, OperationID: "file-alias"}
		if _, err = s.ImportLegacy(ctx, request, confirmation); !errors.Is(err, memory.ErrInvalidInput) {
			t.Errorf("canonical file alias %q accepted: %v", path, err)
		}
		after, e := os.ReadFile(canonical)
		if e != nil || !bytes.Equal(before, after) {
			t.Fatalf("canonical bytes changed %v", e)
		}
	}
}
