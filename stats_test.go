package main

import (
	"path/filepath"
	"testing"
)

func TestStatsStoreSummaryAndRecent(t *testing.T) {
	store, err := NewStatsStore(filepath.Join(t.TempDir(), "stats.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.Record("alice", "photo.jxl", 1000, 600); err != nil {
		t.Fatal(err)
	}
	if err := store.Record("bob", "video.mp4", 3000, 2400); err != nil {
		t.Fatal(err)
	}

	stats, err := store.Summary()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ProcessedCount != 2 || stats.OriginalBytes != 4000 || stats.UploadedBytes != 3000 || stats.SavedBytes != 1000 {
		t.Fatalf("unexpected summary: %+v", stats)
	}
	if stats.Reduction != 25 {
		t.Fatalf("unexpected reduction: %v", stats.Reduction)
	}

	recent, err := store.Recent(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 2 || recent[0].Filename != "video.mp4" {
		t.Fatalf("unexpected recent assets: %+v", recent)
	}
}
