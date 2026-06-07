package taskcontext

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreSetShowClearAndSummary(t *testing.T) {
	store := NewStore(t.TempDir())

	record, err := store.SetHeading("task-a", "  First useful line  ")
	if err != nil {
		t.Fatal(err)
	}
	if record.TaskID != "task-a" || record.Heading != "First useful line" || record.Summary() != "First useful line" {
		t.Fatalf("record = %#v summary=%q", record, record.Summary())
	}
	record, err = store.SetPreview("task-a", "  CI waiting  ")
	if err != nil {
		t.Fatal(err)
	}
	if record.Preview != "CI waiting" || record.Heading != "First useful line" || record.Summary() != "First useful line" {
		t.Fatalf("record after preview = %#v summary=%q", record, record.Summary())
	}
	record, err = store.SetDetail("task-a", "\nsecond line\nthird line\n")
	if err != nil {
		t.Fatal(err)
	}
	if record.Preview != "CI waiting" || record.Heading != "First useful line" || record.Detail != "second line\nthird line" || record.Summary() != "First useful line" {
		t.Fatalf("record after detail = %#v summary=%q", record, record.Summary())
	}

	got, ok, err := store.Show("task-a")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.Preview != record.Preview || got.Heading != record.Heading || got.Detail != record.Detail || got.Summary() != "First useful line" {
		t.Fatalf("show = %#v ok=%t", got, ok)
	}

	removed, err := store.Clear("task-a", "heading")
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("clear should report removed heading context")
	}
	got, ok, err = store.Show("task-a")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.Preview != "CI waiting" || got.Heading != "" || got.Detail != "second line\nthird line" {
		t.Fatalf("detail should remain after heading clear: %#v ok=%t", got, ok)
	}
	if got.Summary() != "CI waiting" {
		t.Fatalf("preview should provide summary after heading clear: %#v summary=%q", got, got.Summary())
	}

	removed, err = store.Clear("task-a", "detail")
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("clear should report removed detail context")
	}
	got, ok, err = store.Show("task-a")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.Preview != "CI waiting" || got.Heading != "" || got.Detail != "" {
		t.Fatalf("preview should remain after detail clear: %#v ok=%t", got, ok)
	}

	removed, err = store.Clear("task-a", "preview")
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("clear should report removed preview context")
	}
	_, ok, err = store.Show("task-a")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("context should be cleared")
	}
	if _, err := os.Stat(store.Path); !os.IsNotExist(err) {
		t.Fatalf("empty context file should be removed, err=%v", err)
	}
}

func TestStoreRejectsMalformedFile(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(store.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path, []byte(`{"version":2,"records":{},"surprise":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := store.Load()
	if err == nil || !strings.Contains(err.Error(), "could not parse task-context.json") {
		t.Fatalf("malformed load error = %v", err)
	}
}

func TestStoreCleanupRemovesStaleTasks(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.SetHeading("keep", "keep this"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetHeading("stale", "remove this"); err != nil {
		t.Fatal(err)
	}

	removed, err := store.Cleanup(map[string]bool{"keep": true})
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	records, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records["keep"].Heading != "keep this" {
		t.Fatalf("records after cleanup = %#v", records)
	}
}

func TestStoreRejectsEmptyAndOversizeWithoutClobberingExisting(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.SetHeading("task-a", "original"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetHeading("task-a", " \n\t "); err == nil || !strings.Contains(err.Error(), "cannot be empty") {
		t.Fatalf("empty error = %v", err)
	}
	if _, err := store.SetHeading("task-a", "one\ntwo"); err == nil || !strings.Contains(err.Error(), "must be one line") {
		t.Fatalf("multi-line heading error = %v", err)
	}
	if _, err := store.SetPreview("task-a", "one\ntwo"); err == nil || !strings.Contains(err.Error(), "must be one line") {
		t.Fatalf("multi-line preview error = %v", err)
	}
	if _, err := store.SetPreview("task-a", strings.Repeat("x", MaxPreviewBytes+1)); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("oversize preview error = %v", err)
	}
	if _, err := store.SetHeading("task-a", strings.Repeat("x", MaxHeadingBytes+1)); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("oversize error = %v", err)
	}
	if _, err := store.SetDetail("task-a", strings.Repeat("x", MaxDetailBytes+1)); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("oversize detail error = %v", err)
	}
	record, ok, err := store.Show("task-a")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || record.Heading != "original" {
		t.Fatalf("existing content should remain after failed writes: %#v ok=%t", record, ok)
	}
}

func TestStoreLoadsLegacyTaskContextVersion(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(store.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path, []byte(`{"version":2,"records":{"task-a":{"task_id":"task-a","heading":"Legacy heading","detail":"Legacy detail","updated_at":"2026-06-06T12:00:00Z"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	records, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	got := records["task-a"]
	if got.Preview != "" || got.Heading != "Legacy heading" || got.Detail != "Legacy detail" || got.Summary() != "Legacy heading" {
		t.Fatalf("legacy record = %#v", got)
	}
}
