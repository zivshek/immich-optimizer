package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type StatsStore struct {
	db *sql.DB
}

type DashboardStats struct {
	ProcessedCount int64   `json:"processed_count"`
	OriginalBytes  int64   `json:"original_bytes"`
	UploadedBytes  int64   `json:"uploaded_bytes"`
	SavedBytes     int64   `json:"saved_bytes"`
	Reduction      float64 `json:"reduction_percent"`
}

type ProcessedAsset struct {
	ID            int64     `json:"id"`
	Profile       string    `json:"profile"`
	Filename      string    `json:"filename"`
	OriginalBytes int64     `json:"original_bytes"`
	UploadedBytes int64     `json:"uploaded_bytes"`
	SavedBytes    int64     `json:"saved_bytes"`
	Reduction     float64   `json:"reduction_percent"`
	ProcessedAt   time.Time `json:"processed_at"`
}

func NewStatsStore(databasePath string) (*StatsStore, error) {
	if err := ensureParentDirectory(databasePath); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, fmt.Errorf("open statistics database: %w", err)
	}

	store := &StatsStore{db: db}
	if err := store.init(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func ensureParentDirectory(databasePath string) error {
	parent := filepath.Dir(databasePath)
	if parent == "." {
		return nil
	}
	if err := os.MkdirAll(parent, 0750); err != nil {
		return fmt.Errorf("create statistics database directory: %w", err)
	}
	return nil
}

func (store *StatsStore) init() error {
	_, err := store.db.Exec(`
		PRAGMA journal_mode = WAL;
		PRAGMA busy_timeout = 5000;
		CREATE TABLE IF NOT EXISTS processed_assets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			profile TEXT NOT NULL,
			filename TEXT NOT NULL,
			original_bytes INTEGER NOT NULL,
			uploaded_bytes INTEGER NOT NULL,
			processed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_processed_assets_processed_at
			ON processed_assets(processed_at DESC);
	`)
	if err != nil {
		return fmt.Errorf("initialize statistics database: %w", err)
	}
	return nil
}

func (store *StatsStore) Close() error {
	return store.db.Close()
}

func (store *StatsStore) Record(profile, filename string, originalBytes, uploadedBytes int64) error {
	_, err := store.db.Exec(
		`INSERT INTO processed_assets (profile, filename, original_bytes, uploaded_bytes) VALUES (?, ?, ?, ?)`,
		profile, filename, originalBytes, uploadedBytes,
	)
	if err != nil {
		return fmt.Errorf("record processed asset: %w", err)
	}
	return nil
}

func (store *StatsStore) Summary() (DashboardStats, error) {
	var stats DashboardStats
	err := store.db.QueryRow(`
		SELECT
			COUNT(*),
			COALESCE(SUM(original_bytes), 0),
			COALESCE(SUM(uploaded_bytes), 0)
		FROM processed_assets
	`).Scan(&stats.ProcessedCount, &stats.OriginalBytes, &stats.UploadedBytes)
	if err != nil {
		return stats, fmt.Errorf("query statistics summary: %w", err)
	}

	stats.SavedBytes = stats.OriginalBytes - stats.UploadedBytes
	if stats.OriginalBytes > 0 {
		stats.Reduction = float64(stats.SavedBytes) / float64(stats.OriginalBytes) * 100
	}
	return stats, nil
}

func (store *StatsStore) Recent(limit int) ([]ProcessedAsset, error) {
	rows, err := store.db.Query(`
		SELECT id, profile, filename, original_bytes, uploaded_bytes, processed_at
		FROM processed_assets
		ORDER BY processed_at DESC, id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query recent processed assets: %w", err)
	}
	defer rows.Close()

	assets := make([]ProcessedAsset, 0, limit)
	for rows.Next() {
		var asset ProcessedAsset
		if err := rows.Scan(
			&asset.ID,
			&asset.Profile,
			&asset.Filename,
			&asset.OriginalBytes,
			&asset.UploadedBytes,
			&asset.ProcessedAt,
		); err != nil {
			return nil, fmt.Errorf("scan recent processed asset: %w", err)
		}
		asset.SavedBytes = asset.OriginalBytes - asset.UploadedBytes
		if asset.OriginalBytes > 0 {
			asset.Reduction = float64(asset.SavedBytes) / float64(asset.OriginalBytes) * 100
		}
		assets = append(assets, asset)
	}
	return assets, rows.Err()
}
