package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// Repository provides methods to work with database
type Repository struct {
	db *DB
}

// OIHistoryRecord represents a record in oi_history table
type OIHistoryRecord struct {
	ID             int64
	Coin           string
	DEX            string
	MarketType     string
	OpenInterest   float64
	OpenInterestUSD float64
	MarkPrice      float64
	Funding        string
	FundingAPR     float64
	Timestamp      time.Time
	CreatedAt      time.Time
}

// AlertRecord represents a record in alerts table
type AlertRecord struct {
	ID            int64
	Coin          string
	DEX           string
	MarketType    string
	PreviousOI    float64
	CurrentOI     float64
	ChangePercent float64
	Direction     string
	Timestamp     time.Time
	CreatedAt     time.Time
}

// NewRepository creates a new repository instance
func NewRepository(db *DB) *Repository {
	return &Repository{db: db}
}

// CalculateFundingAPR calculates the annual percentage rate from funding rate
// Hyperliquid funding is paid every hour (24 times per day)
// API returns hourly funding rate
// APR = funding_rate * 24 * 365 * 100 (as percentage)
func CalculateFundingAPR(fundingRate float64) float64 {
	// Funding every hour = 24 times per day
	// 24 * 365 = 8760 periods per year
	return fundingRate * 24.0 * 365.0 * 100.0
}

// SaveOIHistory saves open interest data to history table with DEX, market type and funding APR
func (r *Repository) SaveOIHistory(coin, dex, marketType string, openInterest, markPrice, funding float64) error {
	fundingAPR := CalculateFundingAPR(funding)
	openInterestUSD := openInterest * markPrice
	
	// Format funding as string with 10 decimal places to avoid scientific notation
	fundingStr := fmt.Sprintf("%.10f", funding)
	
	query := `INSERT INTO oi_history (coin, dex, market_type, open_interest, open_interest_usd, mark_price, funding, funding_apr) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := r.db.conn.Exec(query, coin, dex, marketType, openInterest, openInterestUSD, markPrice, fundingStr, fundingAPR)
	if err != nil {
		return fmt.Errorf("failed to save OI history: %w", err)
	}
	return nil
}

// GetLastOI retrieves the most recent OI record for a given coin and DEX
func (r *Repository) GetLastOI(coin, dex string) (*OIHistoryRecord, error) {
	query := `SELECT id, coin, dex, market_type, open_interest, open_interest_usd, mark_price, funding, funding_apr, timestamp, created_at 
		FROM oi_history 
		WHERE coin = ? AND dex = ?
		ORDER BY timestamp DESC 
		LIMIT 1`

	row := r.db.conn.QueryRow(query, coin, dex)

	var record OIHistoryRecord
	err := row.Scan(&record.ID, &record.Coin, &record.DEX, &record.MarketType,
		&record.OpenInterest, &record.OpenInterestUSD, &record.MarkPrice, &record.Funding, &record.FundingAPR,
		&record.Timestamp, &record.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get last OI: %w", err)
	}

	return &record, nil
}

// SaveAlert saves an alert to the alerts table with DEX and market type
func (r *Repository) SaveAlert(coin, dex, marketType string, previousOI, currentOI, changePercent float64, direction string) error {
	query := `INSERT INTO alerts (coin, dex, market_type, previous_oi, current_oi, change_percent, direction) 
		VALUES (?, ?, ?, ?, ?, ?, ?)`
	_, err := r.db.conn.Exec(query, coin, dex, marketType, previousOI, currentOI, changePercent, direction)
	if err != nil {
		return fmt.Errorf("failed to save alert: %w", err)
	}
	return nil
}

// GetRecentAlerts retrieves recent alerts (last N alerts or within time range)
func (r *Repository) GetRecentAlerts(limit int) ([]AlertRecord, error) {
	query := `SELECT id, coin, dex, market_type, previous_oi, current_oi, change_percent, direction, timestamp, created_at 
		FROM alerts 
		ORDER BY timestamp DESC 
		LIMIT ?`

	rows, err := r.db.conn.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get recent alerts: %w", err)
	}
	defer rows.Close()

	var alerts []AlertRecord
	for rows.Next() {
		var alert AlertRecord
		err := rows.Scan(&alert.ID, &alert.Coin, &alert.DEX, &alert.MarketType,
			&alert.PreviousOI, &alert.CurrentOI, &alert.ChangePercent, &alert.Direction,
			&alert.Timestamp, &alert.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan alert: %w", err)
		}
		alerts = append(alerts, alert)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return alerts, nil
}

// GetOIHistoryForCoin retrieves OI history for a specific coin within time range
func (r *Repository) GetOIHistoryForCoin(coin string, from, to time.Time) ([]OIHistoryRecord, error) {
	query := `SELECT id, coin, dex, market_type, open_interest, open_interest_usd, mark_price, funding, funding_apr, timestamp, created_at 
		FROM oi_history 
		WHERE coin = ? AND timestamp BETWEEN ? AND ?
		ORDER BY timestamp ASC`

	rows, err := r.db.conn.Query(query, coin, from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to get OI history: %w", err)
	}
	defer rows.Close()

	var records []OIHistoryRecord
	for rows.Next() {
		var record OIHistoryRecord
		err := rows.Scan(&record.ID, &record.Coin, &record.DEX, &record.MarketType,
			&record.OpenInterest, &record.OpenInterestUSD, &record.MarkPrice, &record.Funding, &record.FundingAPR,
			&record.Timestamp, &record.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan OI history: %w", err)
		}
		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return records, nil
}

// GetAllCoins retrieves list of all unique coins from history
func (r *Repository) GetAllCoins() ([]string, error) {
	query := `SELECT DISTINCT coin FROM oi_history ORDER BY coin`

	rows, err := r.db.conn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get coins: %w", err)
	}
	defer rows.Close()

	var coins []string
	for rows.Next() {
		var coin string
		if err := rows.Scan(&coin); err != nil {
			return nil, fmt.Errorf("failed to scan coin: %w", err)
		}
		coins = append(coins, coin)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return coins, nil
}

// GetAllDEXs retrieves list of all unique DEXs from history
func (r *Repository) GetAllDEXs() ([]string, error) {
	query := `SELECT DISTINCT dex FROM oi_history ORDER BY dex`

	rows, err := r.db.conn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get DEXs: %w", err)
	}
	defer rows.Close()

	var dexes []string
	for rows.Next() {
		var dex string
		if err := rows.Scan(&dex); err != nil {
			return nil, fmt.Errorf("failed to scan DEX: %w", err)
		}
		dexes = append(dexes, dex)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return dexes, nil
}

// GetOIHistoryForDEX retrieves OI history for a specific DEX
func (r *Repository) GetOIHistoryForDEX(dex string, from, to time.Time) ([]OIHistoryRecord, error) {
	query := `SELECT id, coin, dex, market_type, open_interest, open_interest_usd, mark_price, funding, funding_apr, timestamp, created_at 
		FROM oi_history 
		WHERE dex = ? AND timestamp BETWEEN ? AND ?
		ORDER BY coin, timestamp ASC`

	rows, err := r.db.conn.Query(query, dex, from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to get OI history for DEX: %w", err)
	}
	defer rows.Close()

	var records []OIHistoryRecord
	for rows.Next() {
		var record OIHistoryRecord
		err := rows.Scan(&record.ID, &record.Coin, &record.DEX, &record.MarketType,
			&record.OpenInterest, &record.OpenInterestUSD, &record.MarkPrice, &record.Funding, &record.FundingAPR,
			&record.Timestamp, &record.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan OI history: %w", err)
		}
		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return records, nil
}

// FundingAlertRecord represents a funding alert
type FundingAlertRecord struct {
	ID                 int64
	Coin               string
	DEX                string
	PreviousFunding    float64
	CurrentFunding     float64
	ChangePercent      float64
	PeriodType         string
	PreviousFundingAPR float64
	CurrentFundingAPR  float64
	Timestamp          time.Time
	CreatedAt          time.Time
}

// SaveFundingAlert saves a funding alert to the database
func (r *Repository) SaveFundingAlert(coin, dex string, previousFunding, currentFunding, changePercent float64, periodType string) error {
	prevAPR := CalculateFundingAPR(previousFunding)
	currAPR := CalculateFundingAPR(currentFunding)

	query := `INSERT INTO funding_alerts (coin, dex, previous_funding, current_funding, change_percent, 
		period_type, previous_funding_apr, current_funding_apr) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := r.db.conn.Exec(query, coin, dex, previousFunding, currentFunding, changePercent, periodType, prevAPR, currAPR)
	if err != nil {
		return fmt.Errorf("failed to save funding alert: %w", err)
	}
	return nil
}

// GetFundingAtTime retrieves funding rate for a coin at approximately the specified time
func (r *Repository) GetFundingAtTime(coin, dex string, targetTime time.Time) (float64, time.Time, error) {
	// Get the closest record to target time
	query := `SELECT funding, timestamp FROM oi_history 
		WHERE coin = ? AND dex = ? 
		AND timestamp <= ?
		ORDER BY timestamp DESC 
		LIMIT 1`

	var funding float64
	var timestamp time.Time
	err := r.db.conn.QueryRow(query, coin, dex, targetTime).Scan(&funding, &timestamp)
	if err == sql.ErrNoRows {
		return 0, time.Time{}, nil
	}
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("failed to get funding at time: %w", err)
	}

	return funding, timestamp, nil
}
