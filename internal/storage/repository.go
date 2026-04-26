package storage

import (
	"database/sql"
	"fmt"
	"oi_bot_go/internal/analyzer"
	"strings"
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

// GetOIHistoryForCoinAndDEX retrieves OI history for a specific coin and DEX within time range
func (r *Repository) GetOIHistoryForCoinAndDEX(coin, dex string, from, to time.Time) ([]OIHistoryRecord, error) {
	query := `SELECT id, coin, dex, market_type, open_interest, open_interest_usd, mark_price, funding, funding_apr, timestamp, created_at 
		FROM oi_history 
		WHERE coin = ? AND dex = ? AND timestamp BETWEEN ? AND ?
		ORDER BY timestamp ASC`

	rows, err := r.db.conn.Query(query, coin, dex, from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to get OI history for coin and DEX: %w", err)
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

// SaveInstrumentStats saves or updates statistics
func (r *Repository) SaveInstrumentStats(stats *analyzer.InstrumentStats) error {
	query := `INSERT INTO instrument_stats 
		(coin, dex, funding_mean_14d, funding_stddev_14d, funding_p50_14d,
		 oi_mean_14d, oi_stddev_14d, price_mean_14d, price_volatility_14d, 
		 last_calculated_at, data_points_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(coin, dex) DO UPDATE SET
		funding_mean_14d = excluded.funding_mean_14d,
		funding_stddev_14d = excluded.funding_stddev_14d,
		funding_p50_14d = excluded.funding_p50_14d,
		oi_mean_14d = excluded.oi_mean_14d,
		oi_stddev_14d = excluded.oi_stddev_14d,
		price_mean_14d = excluded.price_mean_14d,
		price_volatility_14d = excluded.price_volatility_14d,
		last_calculated_at = excluded.last_calculated_at,
		data_points_count = excluded.data_points_count`

	_, err := r.db.conn.Exec(query, stats.Coin, stats.DEX, stats.FundingMean,
		stats.FundingStdDev, stats.FundingP50, stats.OIMean, stats.OIStdDev,
		stats.PriceMean, stats.PriceVolatility, stats.LastCalculatedAt,
		stats.DataPointsCount)
	return err
}

// GetInstrumentStats retrieves stats for coin+dex
func (r *Repository) GetInstrumentStats(coin, dex string) (*analyzer.InstrumentStats, error) {
	query := `SELECT coin, dex, funding_mean_14d, funding_stddev_14d, funding_p50_14d,
		oi_mean_14d, oi_stddev_14d, price_mean_14d, price_volatility_14d,
		last_calculated_at, data_points_count
		FROM instrument_stats WHERE coin = ? AND dex = ?`

	row := r.db.conn.QueryRow(query, coin, dex)

	stats := &analyzer.InstrumentStats{Coin: coin, DEX: dex}
	err := row.Scan(&stats.Coin, &stats.DEX, &stats.FundingMean, &stats.FundingStdDev,
		&stats.FundingP50, &stats.OIMean, &stats.OIStdDev, &stats.PriceMean,
		&stats.PriceVolatility, &stats.LastCalculatedAt, &stats.DataPointsCount)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("stats not found")
	}
	return stats, err
}

// SyncStateRecord represents sync state
type SyncStateRecord struct {
	ID                int64
	Coin              string
	DEX               string
	LastFundingValue  float64
	LastFundingUpdate time.Time
	FundingUpdateCount int
	PrevFundingValue  float64
	LastOIUSD         float64
	LastOIUpdate      time.Time
	LastMarkPrice     float64
	LastOraclePrice   float64
	Price30mAgo       float64
	PriceDirection30m float64
}

// GetSyncState retrieves sync state
func (r *Repository) GetSyncState(coin, dex string) (*SyncStateRecord, error) {
	query := `SELECT id, coin, dex, last_funding_value, last_funding_update,
		funding_update_count, prev_funding_value, last_oi_usd, last_oi_update,
		last_mark_price, last_oracle_price, price_30m_ago, price_direction_30m
		FROM instrument_sync_state WHERE coin = ? AND dex = ?`

	row := r.db.conn.QueryRow(query, coin, dex)

	var record SyncStateRecord
	err := row.Scan(&record.ID, &record.Coin, &record.DEX, &record.LastFundingValue,
		&record.LastFundingUpdate, &record.FundingUpdateCount, &record.PrevFundingValue,
		&record.LastOIUSD, &record.LastOIUpdate, &record.LastMarkPrice,
		&record.LastOraclePrice, &record.Price30mAgo, &record.PriceDirection30m)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("sync state not found")
	}
	return &record, err
}

// SaveSyncState saves sync state
func (r *Repository) SaveSyncState(state *analyzer.SyncState) error {
	query := `INSERT INTO instrument_sync_state 
		(coin, dex, last_funding_value, last_funding_update, funding_update_count,
		 prev_funding_value, last_oi_usd, last_oi_update, last_mark_price,
		 last_oracle_price, price_30m_ago, price_direction_30m)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(coin, dex) DO UPDATE SET
		last_funding_value = excluded.last_funding_value,
		last_funding_update = excluded.last_funding_update,
		funding_update_count = excluded.funding_update_count,
		prev_funding_value = excluded.prev_funding_value,
		last_oi_usd = excluded.last_oi_usd,
		last_oi_update = excluded.last_oi_update,
		last_mark_price = excluded.last_mark_price,
		last_oracle_price = excluded.last_oracle_price,
		price_30m_ago = excluded.price_30m_ago,
		price_direction_30m = excluded.price_direction_30m`

	_, err := r.db.conn.Exec(query, state.Coin, state.DEX, state.LastFundingValue,
		state.LastFundingUpdate, state.FundingUpdateCount, state.PrevFundingValue,
		state.LastOIUSD, state.LastOIUpdate, state.LastMarkPrice, state.LastOraclePrice,
		state.Price30mAgo, state.PriceDirection30m)
	return err
}

// SignalQueueRecord represents queued signal
type SignalQueueRecord struct {
	ID                int64
	Coin              string
	DEX               string
	MarketType        string
	SignalDirection   string
	SignalConfidence  string
	OIChange30m       float64
	OIChange2h        float64
	OIChange24h       float64
	OIUSDCurrent      float64
	FundingCurrent    float64
	FundingChangeAbs  float64
	FundingZScore     float64
	FundingAPRCurrent float64
	FundingFresh      bool
	PriceChange30m    float64
	PriceChange2h     float64
	MarkPrice         float64
	OraclePrice       float64
	MarkOracleDelta   float64
	CompositeScore    float64
	DetectedAt        time.Time
	DetectedInWindow  string
	Processed         bool
	SentInDigestAt    *time.Time
}

// SaveSignalToQueue saves signal
func (r *Repository) SaveSignalToQueue(signal *SignalQueueRecord) error {
	query := `INSERT INTO signal_queue 
		(coin, dex, market_type, signal_direction, signal_confidence,
		 oi_change_30m, oi_change_2h, oi_change_24h, oi_usd_current,
		 funding_current, funding_change_abs, funding_zscore, funding_apr_current,
		 funding_fresh, price_change_30m, price_change_2h, mark_price, oracle_price,
		 mark_oracle_delta, composite_score, detected_in_window)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := r.db.conn.Exec(query,
		signal.Coin, signal.DEX, signal.MarketType, signal.SignalDirection,
		signal.SignalConfidence, signal.OIChange30m, signal.OIChange2h,
		signal.OIChange24h, signal.OIUSDCurrent, signal.FundingCurrent,
		signal.FundingChangeAbs, signal.FundingZScore, signal.FundingAPRCurrent,
		signal.FundingFresh, signal.PriceChange30m, signal.PriceChange2h,
		signal.MarkPrice, signal.OraclePrice, signal.MarkOracleDelta,
		signal.CompositeScore, signal.DetectedInWindow)
	return err
}

// GetUnprocessedSignalsByDirection retrieves unprocessed signals by direction
func (r *Repository) GetUnprocessedSignalsByDirection(direction string, limit int) ([]SignalQueueRecord, error) {
	query := `SELECT id, coin, dex, market_type, signal_direction, signal_confidence,
		oi_change_30m, oi_change_2h, oi_change_24h, oi_usd_current,
		funding_current, funding_change_abs, funding_zscore, funding_apr_current,
		funding_fresh, price_change_30m, price_change_2h, mark_price, oracle_price,
		mark_oracle_delta, composite_score, detected_at, detected_in_window,
		processed, sent_in_digest_at
		FROM signal_queue 
		WHERE processed = 0 AND signal_direction = ?
		ORDER BY composite_score DESC LIMIT ?`

	rows, err := r.db.conn.Query(query, direction, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var signals []SignalQueueRecord
	for rows.Next() {
		var s SignalQueueRecord
		err := rows.Scan(&s.ID, &s.Coin, &s.DEX, &s.MarketType, &s.SignalDirection,
			&s.SignalConfidence, &s.OIChange30m, &s.OIChange2h, &s.OIChange24h,
			&s.OIUSDCurrent, &s.FundingCurrent, &s.FundingChangeAbs, &s.FundingZScore,
			&s.FundingAPRCurrent, &s.FundingFresh, &s.PriceChange30m, &s.PriceChange2h,
			&s.MarkPrice, &s.OraclePrice, &s.MarkOracleDelta, &s.CompositeScore,
			&s.DetectedAt, &s.DetectedInWindow, &s.Processed, &s.SentInDigestAt)
		if err != nil {
			return nil, err
		}
		signals = append(signals, s)
	}

	return signals, rows.Err()
}

// MarkSignalsProcessed marks signals as processed with batching for safety
func (r *Repository) MarkSignalsProcessed(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	const batchSize = 500 // SQLite limit safety
	now := time.Now()

	for i := 0; i < len(ids); i += batchSize {
		end := i + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[i:end]

		placeholders := make([]string, len(batch))
		args := make([]interface{}, len(batch)+1)
		args[0] = now

		for j, id := range batch {
			if id <= 0 {
				return fmt.Errorf("invalid signal ID: %d", id)
			}
			placeholders[j] = "?"
			args[j+1] = id
		}

		query := fmt.Sprintf(`UPDATE signal_queue SET processed = 1, sent_in_digest_at = ?
			WHERE id IN (%s)`, strings.Join(placeholders, ","))

		_, err := r.db.conn.Exec(query, args...)
		if err != nil {
			return fmt.Errorf("failed to mark batch %d-%d as processed: %w", i, end, err)
		}
	}
	return nil
}

// GetDB returns the raw database connection for custom queries
func (r *Repository) GetDB() *sql.DB {
	return r.db.conn
}
