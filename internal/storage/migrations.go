package storage

import (
	"fmt"
)

// migrate runs all database migrations
func (db *DB) migrate() error {
	migrations := []struct {
		name string
		sql  string
	}{
		{
			name: "create_oi_history_table_v3",
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
			name: "create_oi_history_indexes",
			sql: `CREATE INDEX IF NOT EXISTS idx_oi_history_coin ON oi_history(coin);
			CREATE INDEX IF NOT EXISTS idx_oi_history_dex ON oi_history(dex);
			CREATE INDEX IF NOT EXISTS idx_oi_history_timestamp ON oi_history(timestamp);
			CREATE INDEX IF NOT EXISTS idx_oi_history_coin_timestamp ON oi_history(coin, timestamp DESC);
			CREATE INDEX IF NOT EXISTS idx_oi_history_dex_coin ON oi_history(dex, coin, timestamp DESC);`,
		},
		{
			name: "create_alerts_table_v2",
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
			name: "create_alerts_indexes",
			sql: `CREATE INDEX IF NOT EXISTS idx_alerts_coin ON alerts(coin);
			CREATE INDEX IF NOT EXISTS idx_alerts_dex ON alerts(dex);
			CREATE INDEX IF NOT EXISTS idx_alerts_timestamp ON alerts(timestamp);
			CREATE INDEX IF NOT EXISTS idx_alerts_coin_timestamp ON alerts(coin, timestamp DESC);
			CREATE INDEX IF NOT EXISTS idx_alerts_dex_coin ON alerts(dex, coin, timestamp DESC);`,
		},
		{
			name: "add_funding_apr_column",
			sql: `ALTER TABLE oi_history ADD COLUMN funding_apr NUMERIC NOT NULL DEFAULT 0;`,
		},
		{
			name: "create_funding_alerts_table",
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
			name: "create_funding_alerts_indexes",
			sql: `CREATE INDEX IF NOT EXISTS idx_funding_alerts_coin ON funding_alerts(coin);
			CREATE INDEX IF NOT EXISTS idx_funding_alerts_dex ON funding_alerts(dex);
			CREATE INDEX IF NOT EXISTS idx_funding_alerts_timestamp ON funding_alerts(timestamp);
			CREATE INDEX IF NOT EXISTS idx_funding_alerts_period ON funding_alerts(period_type);`,
		},
		{
			name: "add_open_interest_usd_column",
			sql: `ALTER TABLE oi_history ADD COLUMN open_interest_usd REAL NOT NULL DEFAULT 0;`,
		},
		// Phase 1: Smart Alerting System - Database Foundation
		{
			name: "create_instrument_stats_table_v4",
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
			name: "create_instrument_sync_state_table_v5",
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
			name: "create_signal_queue_table_v6",
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
		if _, err := db.conn.Exec(migration.sql); err != nil {
			// Ignore errors for duplicate columns
			if migration.name == "add_funding_apr_column" ||
				migration.name == "add_open_interest_usd_column" ||
				migration.name == "change_funding_to_text" {
				continue // Skip if column already exists or type change not supported
			}
			return fmt.Errorf("migration %s failed: %w", migration.name, err)
		}
	}

	return nil
}
