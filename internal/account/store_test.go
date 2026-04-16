package account

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSelectSkipsCoolingAndDisabled(t *testing.T) {
	t.Parallel()
	store, err := NewStore(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	cooling := now.Add(10 * time.Minute)
	for _, item := range []Config{
		{ID: "a1", SourceID: "grok", Status: StatusDisabled},
		{ID: "a2", SourceID: "grok", Status: StatusCooling, CoolingUntil: &cooling, Priority: 10},
		{ID: "a3", SourceID: "grok", Status: StatusActive, Priority: 1},
	} {
		if err := store.Upsert(item); err != nil {
			t.Fatal(err)
		}
	}
	sel, err := store.Select("grok", now)
	if err != nil {
		t.Fatal(err)
	}
	if sel.Account.ID != "a3" {
		t.Fatalf("got %s", sel.Account.ID)
	}
}

func TestMarkFailureAndSuccess(t *testing.T) {
	t.Parallel()
	store, err := NewStore(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(Config{ID: "a1", SourceID: "grok", Status: StatusActive}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.MarkFailure("a1", "boom", 5*time.Minute, now); err != nil {
		t.Fatal(err)
	}
	item := store.List()[0]
	if item.Status != StatusCooling || item.FailureCount != 1 || item.CoolingUntil == nil {
		t.Fatalf("unexpected failure state: %#v", item)
	}
	if err := store.MarkSuccess("a1", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	item = store.List()[0]
	if item.Status != StatusActive || item.FailureCount != 0 || item.UsedRequests != 1 || item.CoolingUntil != nil {
		t.Fatalf("unexpected success state: %#v", item)
	}
}

func TestSelectByPluginFallback(t *testing.T) {
	t.Parallel()
	store, err := NewStore(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(Config{ID: "a1", SourceID: "plugin:echo", PluginID: "echo", Status: StatusActive, Priority: 1}); err != nil {
		t.Fatal(err)
	}
	sel, err := store.SelectByPlugin("echo", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if sel.Account.ID != "a1" {
		t.Fatalf("expected a1, got %s", sel.Account.ID)
	}
}
