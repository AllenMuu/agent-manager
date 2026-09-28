package run

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/policy"
)

func TestStoreUpdateSerializesSeparateInstances(t *testing.T) {
	root := filepath.Join(t.TempDir(), "runs")
	first, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	configured, err := policy.Load(strings.NewReader("version: v1\nkind: agent-policy\nid: concurrent-store\nname: Concurrent Store\ntools:\n  allow: [read_file]\n"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Resolve(configured, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	startedAt := time.Now().UTC()
	record, _, err := first.Start(snapshot, "mock", t.TempDir(), map[policy.Control]bool{
		policy.ControlToolInterception: true,
		policy.ControlRuntimeEvents:    true,
	}, startedAt)
	if err != nil {
		t.Fatal(err)
	}
	firstEvent, err := auditFor(record, policy.Event{Category: policy.BudgetUpdated, Timestamp: startedAt.Add(time.Second)}, policy.Decision{Outcome: policy.Allow}, "")
	if err != nil {
		t.Fatal(err)
	}
	secondEvent, err := auditFor(record, policy.Event{Category: policy.BudgetUpdated, Timestamp: startedAt.Add(2 * time.Second)}, policy.Decision{Outcome: policy.Allow}, "")
	if err != nil {
		t.Fatal(err)
	}

	enteredFirst := make(chan struct{})
	releaseFirst := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseFirst) }) }
	defer release()
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- first.update(func(db *database) error {
			db.Events = append(db.Events, firstEvent)
			close(enteredFirst)
			<-releaseFirst
			return nil
		})
	}()
	select {
	case <-enteredFirst:
	case <-time.After(time.Second):
		t.Fatal("first update did not enter its transaction")
	}

	enteredSecond := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- second.update(func(db *database) error {
			seen := false
			for _, event := range db.Events {
				seen = seen || event.ID == firstEvent.ID
			}
			if !seen {
				return errors.New("second transaction did not observe first update")
			}
			db.Events = append(db.Events, secondEvent)
			close(enteredSecond)
			return nil
		})
	}()
	select {
	case <-enteredSecond:
		release()
		<-firstDone
		<-secondDone
		t.Fatal("second store instance entered while the first held the transaction lock")
	case <-time.After(100 * time.Millisecond):
	}

	release()
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	select {
	case <-enteredSecond:
	case <-time.After(time.Second):
		t.Fatal("second update did not enter after the first released its lock")
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	events, err := first.Events(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	seenEvents := map[string]bool{}
	for _, event := range events {
		seenEvents[event.ID] = true
	}
	if !seenEvents[firstEvent.ID] || !seenEvents[secondEvent.ID] {
		t.Fatalf("stored events lost one concurrent update: %#v", events)
	}
}
