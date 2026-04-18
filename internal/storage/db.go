package storage

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// DB represents a database connection
type DB struct {
	conn *sql.DB
}

// NewDB creates a new database connection and runs migrations
func NewDB(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	db := &DB{conn: conn}

	// Run migrations
	if err := db.migrate(); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return db, nil
}

// Close closes the database connection
func (db *DB) Close() error {
	return db.conn.Close()
}

// Connection returns the underlying sql.DB connection
func (db *DB) Connection() *sql.DB {
	return db.conn
}
