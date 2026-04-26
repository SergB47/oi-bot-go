package storage

import (
	"database/sql"
	"fmt"
)

// ensureMigrationsTable creates the schema_migrations table if it doesn't exist
func (db *DB) ensureMigrationsTable() error {
	query := `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`
	_, err := db.conn.Exec(query)
	return err
}

// isMigrationApplied checks if a migration version has already been applied
func (db *DB) isMigrationApplied(version int) (bool, error) {
	var exists bool
	err := db.conn.QueryRow("SELECT 1 FROM schema_migrations WHERE version = ?", version).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// recordMigration records that a migration has been applied
func (db *DB) recordMigration(tx *sql.Tx, version int) error {
	_, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", version)
	return err
}

// migrate runs all database migrations with transaction support
func (db *DB) migrate() error {
	if err := db.ensureMigrationsTable(); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	migrations := []struct {
		version int
		name    string
		sql     string
	}{
		{
			version: 1,
			name:    "create_oi_history_table_v3",
			sql: `CREATE TABLE IF NOT EXISTS oi_history (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				coin TEXT NOT NULL,
				dex TEXT NOT NULL DEFAULT 'native',
				market_type TEXT NOT NULL DEFAULT 'perp',
				open_interest REAL NOT NULL,
				open_interest_usd REAL NOT NULL DEFAULT 0,
				mark_price REAL NOT NULL,
				funding TEXT NOT NULL,
				funding_apr REAL NOT NULL DEFAULT 0,
				timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP
			);`,
		},
		{
			version: 2,
			name:    "create_oi_history_indexes",
			sql: `CREATE INDEX IF NOT EXISTS idx_oi_history_coin ON oi_history(coin);
			CREATE INDEX IF NOT EXISTS idx_oi_history_dex ON oi_history(dex);
			CREATE INDEX IF NOT EXISTS idx_oi_history_timestamp ON oi_history(timestamp);
			CREATE INDEX IF NOT EXISTS idx_oi_history_coin_timestamp ON oi_history(coin, timestamp DESC);
			CREATE INDEX IF NOT EXISTS idx_oi_history_dex_coin ON oi_history(dex, coin, timestamp DESC);`,
		},
		{
			version: 3,
			name:    "create_alerts_table_v2",
			sql: `CREATE TABLE IF NOT EXISTS alerts (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				coin TEXT NOT NULL,
				dex TEXT NOT NULL DEFAULT 'native',
				market_type TEXT NOT NULL DEFAULT 'perp',
				previous_oi REAL NOT NULL,
				current_oi REAL NOT NULL,
				change_percent REAL NOT NULL,
				direction TEXT NOT NULL CHECK(direction IN ('increase', 'decrease')),
				timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP
			);`,
		},
		{
			version: 4,
			name:    "create_alerts_indexes",
			sql: `CREATE INDEX IF NOT EXISTS idx_alerts_coin ON alerts(coin);
			CREATE INDEX IF NOT EXISTS idx_alerts_dex ON alerts(dex);
			CREATE INDEX IF NOT EXISTS idx_alerts_timestamp ON alerts(timestamp);
			CREATE INDEX IF NOT EXISTS idx_alerts_coin_timestamp ON alerts(coin, timestamp DESC);
			CREATE INDEX IF NOT EXISTS idx_alerts_dex_coin ON alerts(dex, coin, timestamp DESC);`,
		},
		{
			version: 5,
			name:    "add_funding_apr_column",
			sql:     `ALTER TABLE oi_history ADD COLUMN funding_apr NUMERIC NOT NULL DEFAULT 0;`,
		},
		{
			version: 6,
			name:    "create_funding_alerts_table",
			sql: `CREATE TABLE IF NOT EXISTS funding_alerts (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				coin TEXT NOT NULL,
				dex TEXT NOT NULL DEFAULT 'native',
				previous_funding REAL NOT NULL,
				current_funding REAL NOT NULL,
				change_percent REAL NOT NULL,
				period_type TEXT NOT NULL CHECK(period_type IN ('5min', '1hour')),
				previous_funding_apr REAL NOT NULL,
				current_funding_apr REAL NOT NULL,
				timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP
			);`,
		},
		{
			version: 7,
			name:    "create_funding_alerts_indexes",
			sql: `CREATE INDEX IF NOT EXISTS idx_funding_alerts_coin ON funding_alerts(coin);
			CREATE INDEX IF NOT EXISTS idx_funding_alerts_dex ON funding_alerts(dex);
			CREATE INDEX IF NOT EXISTS idx_funding_alerts_timestamp ON funding_alerts(timestamp);
			CREATE INDEX IF NOT EXISTS idx_funding_alerts_period ON funding_alerts(period_type);`,
		},
		{
			version: 8,
			name:    "add_open_interest_usd_column",
			sql:     `ALTER TABLE oi_history ADD COLUMN open_interest_usd REAL NOT NULL DEFAULT 0;`,
		},
		// Phase 1: Smart Alerting System - Database Foundation
		{
			version: 9,
			name:    "create_instrument_stats_table_v4",
			sql: `CREATE TABLE IF NOT EXISTS instrument_stats (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				coin TEXT NOT NULL,
				dex TEXT NOT NULL DEFAULT 'native',
				funding_mean_14d REAL DEFAULT 0,
				funding_stddev_14d REAL DEFAULT 0,
				funding_p50_14d REAL DEFAULT 0,
				funding_p95_14d REAL DEFAULT 0,
				funding_p5_14d REAL DEFAULT 0,
				oi_mean_14d REAL DEFAULT 0,
				oi_stddev_14d REAL DEFAULT 0,
				oi_max_14d REAL DEFAULT 0,
				oi_min_14d REAL DEFAULT 0,
				price_mean_14d REAL DEFAULT 0,
				price_volatility_14d REAL DEFAULT 0,
				last_calculated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				data_points_count INTEGER DEFAULT 0,
				UNIQUE(coin, dex)
			);
			CREATE INDEX IF NOT EXISTS idx_instrument_stats_coin_dex ON instrument_stats(coin, dex);
			CREATE INDEX IF NOT EXISTS idx_instrument_stats_last_calc ON instrument_stats(last_calculated_at);`,
		},
		{
			version: 10,
			name:    "create_instrument_sync_state_table_v5",
			sql: `CREATE TABLE IF NOT EXISTS instrument_sync_state (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				coin TEXT NOT NULL,
				dex TEXT NOT NULL DEFAULT 'native',
				last_funding_value REAL DEFAULT 0,
				last_funding_update DATETIME,
				funding_update_count INTEGER DEFAULT 0,
				prev_funding_value REAL DEFAULT 0,
				last_oi_usd REAL DEFAULT 0,
				last_oi_update DATETIME,
				last_mark_price REAL DEFAULT 0,
				last_oracle_price REAL DEFAULT 0,
				price_30m_ago REAL DEFAULT 0,
				price_direction_30m REAL DEFAULT 0,
				UNIQUE(coin, dex)
			);
			CREATE INDEX IF NOT EXISTS idx_sync_state_coin_dex ON instrument_sync_state(coin, dex);`,
		},
		{
			version: 11,
			name:    "create_signal_queue_table_v6",
			sql: `CREATE TABLE IF NOT EXISTS signal_queue (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				coin TEXT NOT NULL,
				dex TEXT NOT NULL,
				market_type TEXT DEFAULT 'perp',
				signal_direction TEXT CHECK(signal_direction IN ('long', 'short', 'uncertain')),
				signal_confidence TEXT CHECK(signal_confidence IN ('high', 'medium', 'low')),
				oi_change_30m REAL DEFAULT 0,
				oi_change_2h REAL DEFAULT 0,
				oi_change_24h REAL DEFAULT 0,
				oi_usd_current REAL DEFAULT 0,
				funding_current REAL DEFAULT 0,
				funding_change_abs REAL DEFAULT 0,
				funding_zscore REAL DEFAULT 0,
				funding_apr_current REAL DEFAULT 0,
				funding_fresh BOOLEAN DEFAULT 0,
				price_change_30m REAL DEFAULT 0,
				price_change_2h REAL DEFAULT 0,
				mark_price REAL DEFAULT 0,
				oracle_price REAL DEFAULT 0,
				mark_oracle_delta REAL DEFAULT 0,
				composite_score REAL DEFAULT 0,
				detected_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				detected_in_window TEXT CHECK(detected_in_window IN ('instant', '30min', '2hour', '24hour')),
				processed BOOLEAN DEFAULT 0,
				sent_in_digest_at DATETIME,
				UNIQUE(coin, dex, detected_at)
			);
			CREATE INDEX IF NOT EXISTS idx_signal_queue_unprocessed ON signal_queue(processed, composite_score DESC);
			CREATE INDEX IF NOT EXISTS idx_signal_queue_direction ON signal_queue(signal_direction, processed);`,
		},
	}

	for _, migration := range migrations {
		// Check if already applied
		applied, err := db.isMigrationApplied(migration.version)
		if err != nil {
			return fmt.Errorf("failed to check migration %d status: %w", migration.version, err)
		}
		if applied {
			continue // Skip already applied migrations
		}

		// Run in transaction
		tx, err := db.conn.Begin()
		if err != nil {
			return fmt.Errorf("migration %d (%s): failed to begin transaction: %w", migration.version, migration.name, err)
		}

		if _, err := tx.Exec(migration.sql); err != nil {
			tx.Rollback()
			// Ignore errors for duplicate columns (SQLite doesn't support IF NOT EXISTS on ALTER TABLE)
			if migration.name == "add_funding_apr_column" ||
				migration.name == "add_open_interest_usd_column" ||
				migration.name == "change_funding_to_text" {
				// Record as applied outside of the failed transaction
				_, err := db.conn.Exec("INSERT INTO schema_migrations (version) VALUES (?)", migration.version)
				if err != nil {
					return fmt.Errorf("migration %d (%s): failed to record after duplicate column: %w", migration.version, migration.name, err)
				}
				continue
			}
			return fmt.Errorf("migration %d (%s) failed: %w", migration.version, migration.name, err)
		}

		// Record migration
		if err := db.recordMigration(tx, migration.version); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d (%s): failed to record: %w", migration.version, migration.name, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migration %d (%s): failed to commit: %w", migration.version, migration.name, err)
		}
	}

	return nil
}
