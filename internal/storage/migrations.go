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
