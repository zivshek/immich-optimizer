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
	Resolution    string    `json:"resolution"`
	Success       bool      `json:"success"`
	Error         string    `json:"error,omitempty"`
	OriginalBytes int64     `json:"original_bytes"`
	UploadedBytes int64     `json:"uploaded_bytes"`
	SavedBytes    int64     `json:"saved_bytes"`
	Reduction     float64   `json:"reduction_percent"`
	ProcessedAt   time.Time `json:"processed_at"`
}

type RecentJobs struct {
	Jobs       []ProcessedAsset `json:"jobs"`
	Page       int              `json:"page"`
	PageSize   int              `json:"page_size"`
	Total      int64            `json:"total"`
	TotalPages int              `json:"total_pages"`
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
			resolution TEXT NOT NULL DEFAULT '',
			original_bytes INTEGER NOT NULL,
			uploaded_bytes INTEGER NOT NULL,
			processed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_processed_assets_processed_at
			ON processed_assets(processed_at DESC);
		CREATE TABLE IF NOT EXISTS task_config_usage (
			config_name TEXT PRIMARY KEY,
			last_used DATETIME NOT NULL
		);
		CREATE TABLE IF NOT EXISTS profile_task_configs (
			profile TEXT PRIMARY KEY,
			config_name TEXT NOT NULL,
			selected_at DATETIME NOT NULL
		);
		CREATE TABLE IF NOT EXISTS media_task_config_usage (
			media_type TEXT NOT NULL,
			config_name TEXT NOT NULL,
			last_used DATETIME NOT NULL,
			PRIMARY KEY (media_type, config_name)
		);
		CREATE TABLE IF NOT EXISTS profile_media_task_configs (
			profile TEXT NOT NULL,
			media_type TEXT NOT NULL,
			config_name TEXT NOT NULL,
			selected_at DATETIME NOT NULL,
			PRIMARY KEY (profile, media_type)
		);
	`)
	if err != nil {
		return fmt.Errorf("initialize statistics database: %w", err)
	}
	for _, column := range []struct {
		name       string
		definition string
	}{
		{name: "resolution", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "success", definition: "INTEGER NOT NULL DEFAULT 1"},
		{name: "error", definition: "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := store.ensureColumn("processed_assets", column.name, column.definition); err != nil {
			return err
		}
	}
	return nil
}

func (store *StatsStore) RecordMediaTaskConfigSelection(profile, mediaType, configName string) error {
	now := time.Now().UTC()
	tx, err := store.db.Begin()
	if err != nil {
		return fmt.Errorf("begin media task config selection: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`
		INSERT INTO profile_media_task_configs (profile, media_type, config_name, selected_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(profile, media_type) DO UPDATE SET config_name = excluded.config_name, selected_at = excluded.selected_at
	`, profile, mediaType, configName, now); err != nil {
		return fmt.Errorf("record profile media task config: %w", err)
	}
	if _, err := tx.Exec(`
		INSERT INTO media_task_config_usage (media_type, config_name, last_used)
		VALUES (?, ?, ?)
		ON CONFLICT(media_type, config_name) DO UPDATE SET last_used = excluded.last_used
	`, mediaType, configName, now); err != nil {
		return fmt.Errorf("record media task config usage: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit media task config selection: %w", err)
	}
	return nil
}

func (store *StatsStore) SelectedMediaTaskConfig(profile, mediaType string) (string, error) {
	var configName string
	err := store.db.QueryRow(`
		SELECT config_name FROM profile_media_task_configs
		WHERE profile = ? AND media_type = ?
	`, profile, mediaType).Scan(&configName)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("query selected media task config: %w", err)
	}
	return configName, nil
}

func (store *StatsStore) MediaTaskConfigUsage(mediaType string) (map[string]time.Time, error) {
	rows, err := store.db.Query(`
		SELECT config_name, last_used FROM media_task_config_usage
		WHERE media_type = ?
	`, mediaType)
	if err != nil {
		return nil, fmt.Errorf("query media task config usage: %w", err)
	}
	defer rows.Close()
	usage := make(map[string]time.Time)
	for rows.Next() {
		var name string
		var lastUsed time.Time
		if err := rows.Scan(&name, &lastUsed); err != nil {
			return nil, fmt.Errorf("scan media task config usage: %w", err)
		}
		usage[name] = lastUsed
	}
	return usage, rows.Err()
}

func (store *StatsStore) RecordTaskConfigSelection(profile, configName string) error {
	now := time.Now().UTC()
	tx, err := store.db.Begin()
	if err != nil {
		return fmt.Errorf("begin task config selection: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`
		INSERT INTO profile_task_configs (profile, config_name, selected_at)
		VALUES (?, ?, ?)
		ON CONFLICT(profile) DO UPDATE SET config_name = excluded.config_name, selected_at = excluded.selected_at
	`, profile, configName, now); err != nil {
		return fmt.Errorf("record profile task config: %w", err)
	}
	if _, err := tx.Exec(`
		INSERT INTO task_config_usage (config_name, last_used)
		VALUES (?, ?)
		ON CONFLICT(config_name) DO UPDATE SET last_used = excluded.last_used
	`, configName, now); err != nil {
		return fmt.Errorf("record task config usage: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit task config selection: %w", err)
	}
	return nil
}

func (store *StatsStore) SelectedTaskConfig(profile string) (string, error) {
	var configName string
	err := store.db.QueryRow(`SELECT config_name FROM profile_task_configs WHERE profile = ?`, profile).Scan(&configName)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("query selected task config: %w", err)
	}
	return configName, nil
}

func (store *StatsStore) TaskConfigUsage() (map[string]time.Time, error) {
	rows, err := store.db.Query(`SELECT config_name, last_used FROM task_config_usage`)
	if err != nil {
		return nil, fmt.Errorf("query task config usage: %w", err)
	}
	defer rows.Close()
	usage := make(map[string]time.Time)
	for rows.Next() {
		var name string
		var lastUsed time.Time
		if err := rows.Scan(&name, &lastUsed); err != nil {
			return nil, fmt.Errorf("scan task config usage: %w", err)
		}
		usage[name] = lastUsed
	}
	return usage, rows.Err()
}

func (store *StatsStore) Close() error {
	return store.db.Close()
}

func (store *StatsStore) ensureColumn(table, column, definition string) error {
	rows, err := store.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return fmt.Errorf("inspect statistics database schema: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return fmt.Errorf("scan statistics database schema: %w", err)
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("inspect statistics database schema: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close statistics database schema inspection: %w", err)
	}

	if _, err := store.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition)); err != nil {
		return fmt.Errorf("migrate statistics database schema: %w", err)
	}
	return nil
}

func (store *StatsStore) Record(profile, filename, resolution string, originalBytes, uploadedBytes int64) error {
	_, err := store.db.Exec(
		`INSERT INTO processed_assets (profile, filename, resolution, success, error, original_bytes, uploaded_bytes) VALUES (?, ?, ?, 1, '', ?, ?)`,
		profile, filename, resolution, originalBytes, uploadedBytes,
	)
	if err != nil {
		return fmt.Errorf("record processed asset: %w", err)
	}
	return nil
}

func (store *StatsStore) RecordFailure(profile, filename, resolution string, originalBytes int64, jobError error) error {
	errorMessage := ""
	if jobError != nil {
		errorMessage = jobError.Error()
	}
	_, err := store.db.Exec(
		`INSERT INTO processed_assets (profile, filename, resolution, success, error, original_bytes, uploaded_bytes) VALUES (?, ?, ?, 0, ?, ?, 0)`,
		profile, filename, resolution, errorMessage, originalBytes,
	)
	if err != nil {
		return fmt.Errorf("record failed job: %w", err)
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
		WHERE success = 1
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
	recent, err := store.RecentPage(1, limit)
	return recent.Jobs, err
}

func (store *StatsStore) RecentPage(page, pageSize int) (RecentJobs, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}

	var recent RecentJobs
	recent.Page = page
	recent.PageSize = pageSize
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM processed_assets`).Scan(&recent.Total); err != nil {
		return recent, fmt.Errorf("count recent jobs: %w", err)
	}
	recent.TotalPages = int((recent.Total + int64(pageSize) - 1) / int64(pageSize))
	if recent.TotalPages == 0 {
		recent.TotalPages = 1
	}

	rows, err := store.db.Query(`
		SELECT id, profile, filename, resolution, success, error, original_bytes, uploaded_bytes, processed_at
		FROM processed_assets
		ORDER BY processed_at DESC, id DESC
		LIMIT ? OFFSET ?
	`, pageSize, (page-1)*pageSize)
	if err != nil {
		return recent, fmt.Errorf("query recent jobs: %w", err)
	}
	defer rows.Close()

	recent.Jobs = make([]ProcessedAsset, 0, pageSize)
	for rows.Next() {
		var asset ProcessedAsset
		if err := rows.Scan(
			&asset.ID,
			&asset.Profile,
			&asset.Filename,
			&asset.Resolution,
			&asset.Success,
			&asset.Error,
			&asset.OriginalBytes,
			&asset.UploadedBytes,
			&asset.ProcessedAt,
		); err != nil {
			return recent, fmt.Errorf("scan recent job: %w", err)
		}
		if asset.Success {
			asset.SavedBytes = asset.OriginalBytes - asset.UploadedBytes
		}
		if asset.Success && asset.OriginalBytes > 0 {
			asset.Reduction = float64(asset.SavedBytes) / float64(asset.OriginalBytes) * 100
		}
		recent.Jobs = append(recent.Jobs, asset)
	}
	return recent, rows.Err()
}

func (store *StatsStore) Delete(id int64) error {
	result, err := store.db.Exec(`DELETE FROM processed_assets WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete recent job: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check deleted recent job: %w", err)
	}
	if deleted == 0 {
		return sql.ErrNoRows
	}
	return nil
}
