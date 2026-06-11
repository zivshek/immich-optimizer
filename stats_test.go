package main

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestStatsStoreSummaryAndRecent(t *testing.T) {
	store, err := NewStatsStore(filepath.Join(t.TempDir(), "stats.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.Record("alice", "photo.jxl", "4032x3024", 1000, 600); err != nil {
		t.Fatal(err)
	}
	if err := store.Record("bob", "video.mp4", "3840x2160", 3000, 2400); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordFailure("bob", "failed.mp4", "2160x3840", 5000, fmt.Errorf("projected savings below 20%%")); err != nil {
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
	if len(recent) != 3 || recent[0].Filename != "failed.mp4" || recent[0].Success || recent[0].Resolution != "2160x3840" {
		t.Fatalf("unexpected recent assets: %+v", recent)
	}
}

func TestStatsStorePaginationAndDelete(t *testing.T) {
	store, err := NewStatsStore(filepath.Join(t.TempDir(), "stats.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	for i := 0; i < 12; i++ {
		if err := store.Record("alice", fmt.Sprintf("photo-%02d.jpg", i), "", 1000, 900); err != nil {
			t.Fatal(err)
		}
	}
	page, err := store.RecentPage(2, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Jobs) != 2 || page.Total != 12 || page.TotalPages != 2 {
		t.Fatalf("unexpected page: %+v", page)
	}
	if err := store.Delete(page.Jobs[0].ID); err != nil {
		t.Fatal(err)
	}
	page, err = store.RecentPage(2, 10)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 11 || len(page.Jobs) != 1 {
		t.Fatalf("unexpected page after delete: %+v", page)
	}
}

func TestStatsStoreMigratesJobColumns(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "stats.db")
	store, err := NewStatsStore(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()

	store, err = NewStatsStore(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Record("alice", "photo.jpg", "1920x1080", 1000, 900); err != nil {
		t.Fatal(err)
	}
}

func TestStatsStorePersistsIndependentMediaTaskConfigs(t *testing.T) {
	store, err := NewStatsStore(filepath.Join(t.TempDir(), "stats.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.RecordMediaTaskConfigSelection("alice", "image", "images/webp"); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordMediaTaskConfigSelection("alice", "video", "videos/svt-av1"); err != nil {
		t.Fatal(err)
	}
	imageConfig, err := store.SelectedMediaTaskConfig("alice", "image")
	if err != nil || imageConfig != "images/webp" {
		t.Fatalf("unexpected image config: %q, %v", imageConfig, err)
	}
	videoConfig, err := store.SelectedMediaTaskConfig("alice", "video")
	if err != nil || videoConfig != "videos/svt-av1" {
		t.Fatalf("unexpected video config: %q, %v", videoConfig, err)
	}
	if err := store.SetProfileNvidia("alice", true); err != nil {
		t.Fatal(err)
	}
	enabled, err := store.ProfileNvidia("alice")
	if err != nil || !enabled {
		t.Fatalf("unexpected NVIDIA option: %v, %v", enabled, err)
	}
	if err := store.SetProfileImageScore("alice", 87); err != nil {
		t.Fatal(err)
	}
	score, err := store.ProfileImageScore("alice")
	if err != nil || score != 87 {
		t.Fatalf("unexpected image score: %d, %v", score, err)
	}
	if err := store.SetProfileVideoScore("alice", 93); err != nil {
		t.Fatal(err)
	}
	videoScore, err := store.ProfileVideoScore("alice")
	if err != nil || videoScore != 93 {
		t.Fatalf("unexpected video score: %d, %v", videoScore, err)
	}
	if err := store.SetProfileVideoCRF("alice", 30); err != nil {
		t.Fatal(err)
	}
	videoCRF, err := store.ProfileVideoCRF("alice")
	if err != nil || videoCRF != 30 {
		t.Fatalf("unexpected video CRF: %d, %v", videoCRF, err)
	}
}
