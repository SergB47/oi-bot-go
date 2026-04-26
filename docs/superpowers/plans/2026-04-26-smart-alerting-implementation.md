# Smart Alerting System — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement momentum-based alerting system that detects insider front-running in perps. Uses OI + funding correlation for direction, with price-based fallback when funding stale.

**Architecture:** Extend existing scheduler with direction detector (funding-based + price-based fallback), stats calculator, multi-window signal detector, and momentum-focused digest message builder.

**Tech Stack:** Go 1.21, SQLite (modernc.org/sqlite), existing Telegram bot API

---

## Phase 1: Database Foundation

### Task 1: Add Migration for instrument_stats Table

**Files:**
- Modify: `internal/storage/migrations.go`

- [ ] **Step 1: Add migration function for instrument_stats**

```go
func (m *Migrator) migrateV4() error {
    query := `
    CREATE TABLE IF NOT EXISTS instrument_stats (
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
    CREATE INDEX IF NOT EXISTS idx_instrument_stats_last_calc ON instrument_stats(last_calculated_at);
    `
    _, err := m.db.Exec(query)
    return err
}
```

- [ ] **Step 2: Add to migration list and test**

```bash
go run ./cmd/monitor -once -debug
git add internal/storage/migrations.go
git commit -m "feat: add instrument_stats table with price stats"
```

---

### Task 2: Add Migration for instrument_sync_state Table (Updated with Price)

**Files:**
- Modify: `internal/storage/migrations.go`

- [ ] **Step 1: Add migration with price tracking**

```go
func (m *Migrator) migrateV5() error {
    query := `
    CREATE TABLE IF NOT EXISTS instrument_sync_state (
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
    CREATE INDEX IF NOT EXISTS idx_sync_state_coin_dex ON instrument_sync_state(coin, dex);
    `
    _, err := m.db.Exec(query)
    return err
}
```

- [ ] **Step 2: Test and commit**

```bash
go run ./cmd/monitor -once -debug
git add internal/storage/migrations.go
git commit -m "feat: add instrument_sync_state with price direction tracking"
```

---

### Task 3: Add Migration for signal_queue Table (Updated for Momentum)

**Files:**
- Modify: `internal/storage/migrations.go`

- [ ] **Step 1: Add migration with direction and confidence**

```go
func (m *Migrator) migrateV6() error {
    query := `
    CREATE TABLE IF NOT EXISTS signal_queue (
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
    CREATE INDEX IF NOT EXISTS idx_signal_queue_direction ON signal_queue(signal_direction, processed);
    `
    _, err := m.db.Exec(query)
    return err
}
```

- [ ] **Step 2: Test and commit**

```bash
go run ./cmd/monitor -once -debug
git add internal/storage/migrations.go
git commit -m "feat: add signal_queue with direction and confidence fields"
```

---

## Phase 2: Direction Detection Engine

### Task 4: Create Direction Detector

**Files:**
- Create: `internal/analyzer/direction.go`
- Create: `internal/analyzer/types.go`

- [ ] **Step 1: Create types for direction detection**

```go
// internal/analyzer/types.go
package analyzer

// Direction represents trading direction
type Direction string

const (
    DirectionLong       Direction = "long"
    DirectionShort      Direction = "short"
    DirectionUncertain  Direction = "uncertain"
    DirectionUnknown    Direction = "unknown"
)

// SignalConfidence represents confidence level
type SignalConfidence string

const (
    ConfidenceHigh   SignalConfidence = "high"
    ConfidenceMedium SignalConfidence = "medium"
    ConfidenceLow    SignalConfidence = "low"
)

// DirectionResult holds detection result
type DirectionResult struct {
    Direction   Direction
    Confidence  SignalConfidence
    Method      string  // "funding" or "price" or "mixed"
}

// DirectionInput holds all inputs for detection
type DirectionInput struct {
    OIChange30m           float64
    PriceChange30m        float64
    FundingCurrent        float64
    FundingPrevious       float64
    FundingFresh          bool
    MarkPrice             float64
    OraclePrice           float64
    HistoricalFundingMean float64  // 14-day mean
}
```

- [ ] **Step 2: Create direction detector**

```go
// internal/analyzer/direction.go
package analyzer

import "math"

// DirectionDetector determines trade direction
type DirectionDetector struct {
    fundingFreshThresholdMinutes float64
}

// NewDirectionDetector creates detector
func NewDirectionDetector() *DirectionDetector {
    return &DirectionDetector{
        fundingFreshThresholdMinutes: 10.0,
    }
}

// Detect determines direction using best available method
func (dd *DirectionDetector) Detect(input DirectionInput) *DirectionResult {
    // Try funding-based first
    if input.FundingFresh {
        fundingDir := dd.detectByFunding(input.FundingCurrent, input.FundingPrevious)
        if fundingDir != DirectionUnknown {
            return &DirectionResult{
                Direction:  fundingDir,
                Confidence: ConfidenceHigh,
                Method:     "funding",
            }
        }
    }
    
    // Fallback to price-based
    priceDir := dd.detectByPrice(input)
    
    // Calculate confidence based on agreement with historical bias
    confidence := dd.calculateConfidence(input, priceDir)
    
    if priceDir != DirectionUncertain {
        return &DirectionResult{
            Direction:  priceDir,
            Confidence: confidence,
            Method:     "price",
        }
    }
    
    // Check historical bias as last resort
    if input.HistoricalFundingMean > 0.0001 {
        return &DirectionResult{
            Direction:  DirectionLong,
            Confidence: ConfidenceLow,
            Method:     "historical_bias",
        }
    } else if input.HistoricalFundingMean < -0.0001 {
        return &DirectionResult{
            Direction:  DirectionShort,
            Confidence: ConfidenceLow,
            Method:     "historical_bias",
        }
    }
    
    return &DirectionResult{
        Direction:  DirectionUncertain,
        Confidence: ConfidenceLow,
        Method:     "none",
    }
}

func (dd *DirectionDetector) detectByFunding(current, previous float64) Direction {
    // Funding rising and positive = more longs paying = LONG impulse
    if current > 0 && current > previous {
        return DirectionLong
    }
    
    // Funding falling and negative = more shorts paying = SHORT impulse
    if current < 0 && current < previous {
        return DirectionShort
    }
    
    // Funding positive but stable/falling = uncertain
    // Funding negative but stable/rising = uncertain
    return DirectionUnknown
}

func (dd *DirectionDetector) detectByPrice(input DirectionInput) Direction {
    markOracleDelta := 0.0
    if input.OraclePrice > 0 {
        markOracleDelta = (input.MarkPrice - input.OraclePrice) / input.OraclePrice * 100
    }
    
    // Strong correlation: OI rising + Price rising = LONG
    if input.OIChange30m > 5.0 && input.PriceChange30m > 1.0 {
        return DirectionLong
    }
    
    // Strong correlation: OI rising + Price falling = SHORT
    if input.OIChange30m > 5.0 && input.PriceChange30m < -1.0 {
        return DirectionShort
    }
    
    // Mark price premium/discount
    if markOracleDelta > 0.5 && input.OIChange30m > 5.0 {
        return DirectionLong
    }
    if markOracleDelta < -0.5 && input.OIChange30m > 5.0 {
        return DirectionShort
    }
    
    return DirectionUncertain
}

func (dd *DirectionDetector) calculateConfidence(input DirectionInput, priceDir Direction) SignalConfidence {
    if input.FundingFresh {
        // Should not happen - funding would have been used
        return ConfidenceMedium
    }
    
    // Price direction agrees with historical bias = medium confidence
    if priceDir != DirectionUncertain {
        if (priceDir == DirectionLong && input.HistoricalFundingMean > 0) ||
           (priceDir == DirectionShort && input.HistoricalFundingMean < 0) {
            return ConfidenceMedium
        }
        return ConfidenceLow
    }
    
    return ConfidenceLow
}
```

- [ ] **Step 3: Create unit tests for direction detector**

```go
// internal/analyzer/direction_test.go
package analyzer

import "testing"

func TestDirectionDetector_Detect(t *testing.T) {
    dd := NewDirectionDetector()
    
    tests := []struct {
        name  string
        input DirectionInput
        want  Direction
        conf  SignalConfidence
    }{
        {
            name: "Fresh funding positive rising = LONG",
            input: DirectionInput{
                FundingFresh:    true,
                FundingCurrent:  0.0003,
                FundingPrevious: 0.0001,
            },
            want: DirectionLong,
            conf: ConfidenceHigh,
        },
        {
            name: "Fresh funding negative falling = SHORT",
            input: DirectionInput{
                FundingFresh:    true,
                FundingCurrent:  -0.0003,
                FundingPrevious: -0.0001,
            },
            want: DirectionShort,
            conf: ConfidenceHigh,
        },
        {
            name: "Stale funding + price up + OI up = LONG",
            input: DirectionInput{
                FundingFresh:    false,
                OIChange30m:     15.0,
                PriceChange30m:  2.0,
                HistoricalFundingMean: 0.0001,
            },
            want: DirectionLong,
            conf: ConfidenceMedium,
        },
        {
            name: "Stale funding + price down + OI up = SHORT",
            input: DirectionInput{
                FundingFresh:    false,
                OIChange30m:     15.0,
                PriceChange30m:  -2.0,
                HistoricalFundingMean: -0.0001,
            },
            want: DirectionShort,
            conf: ConfidenceMedium,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := dd.Detect(tt.input)
            if got.Direction != tt.want {
                t.Errorf("Detect() direction = %v, want %v", got.Direction, tt.want)
            }
            if got.Confidence != tt.conf {
                t.Errorf("Detect() confidence = %v, want %v", got.Confidence, tt.conf)
            }
        })
    }
}
```

- [ ] **Step 4: Run tests and commit**

```bash
go test ./internal/analyzer/...
git add internal/analyzer/direction.go internal/analyzer/direction_test.go internal/analyzer/types.go
git commit -m "feat: add direction detector with funding and price fallback"
```

---

## Phase 3: Statistics & Analysis

### Task 5: Create Stats Calculator

**Files:**
- Create: `internal/analyzer/stats.go`

- [ ] **Step 1: Implement stats calculator with price stats**

```go
// internal/analyzer/stats.go
package analyzer

import (
    "fmt"
    "math"
    "oi_bot_go/internal/storage"
    "sort"
    "strconv"
    "time"
)

// InstrumentStats holds rolling 14-day statistics
type InstrumentStats struct {
    Coin              string
    DEX               string
    FundingMean       float64
    FundingStdDev     float64
    FundingP50        float64
    OIMean            float64
    OIStdDev          float64
    PriceMean         float64
    PriceVolatility   float64
    LastCalculatedAt  time.Time
    DataPointsCount   int
}

// StatsCalculator calculates rolling statistics
type StatsCalculator struct {
    repository *storage.Repository
}

// NewStatsCalculator creates new calculator
func NewStatsCalculator(repo *storage.Repository) *StatsCalculator {
    return &StatsCalculator{repository: repo}
}

// CalculateStats computes 14-day rolling stats
func (sc *StatsCalculator) CalculateStats(coin, dex string) (*InstrumentStats, error) {
    from := time.Now().AddDate(0, 0, -14)
    to := time.Now()
    
    records, err := sc.repository.GetOIHistoryForCoinAndDEX(coin, dex, from, to)
    if err != nil {
        return nil, fmt.Errorf("failed to get history: %w", err)
    }
    
    if len(records) < 20 {
        return nil, fmt.Errorf("insufficient data: %d records", len(records))
    }
    
    var fundingValues []float64
    var oiValues []float64
    var priceValues []float64
    
    for _, r := range records {
        funding, _ := strconv.ParseFloat(r.Funding, 64)
        fundingValues = append(fundingValues, funding)
        oiValues = append(oiValues, r.OpenInterestUSD)
        priceValues = append(priceValues, r.MarkPrice)
    }
    
    stats := &InstrumentStats{
        Coin:             coin,
        DEX:              dex,
        FundingMean:      mean(fundingValues),
        FundingStdDev:    stdDev(fundingValues),
        FundingP50:       percentile(fundingValues, 0.5),
        OIMean:           mean(oiValues),
        OIStdDev:         stdDev(oiValues),
        PriceMean:        mean(priceValues),
        PriceVolatility:  stdDev(priceValues) / mean(priceValues),
        LastCalculatedAt: time.Now(),
        DataPointsCount:  len(records),
    }
    
    return stats, nil
}

func mean(values []float64) float64 {
    if len(values) == 0 {
        return 0
    }
    sum := 0.0
    for _, v := range values {
        sum += v
    }
    return sum / float64(len(values))
}

func stdDev(values []float64) float64 {
    if len(values) < 2 {
        return 0
    }
    m := mean(values)
    sum := 0.0
    for _, v := range values {
        diff := v - m
        sum += diff * diff
    }
    variance := sum / float64(len(values)-1)
    return math.Sqrt(variance)
}

func percentile(values []float64, p float64) float64 {
    if len(values) == 0 {
        return 0
    }
    sorted := make([]float64, len(values))
    copy(sorted, values)
    sort.Float64s(sorted)
    
    index := p * float64(len(sorted)-1)
    lower := int(math.Floor(index))
    upper := int(math.Ceil(index))
    
    if lower == upper {
        return sorted[lower]
    }
    
    frac := index - float64(lower)
    return sorted[lower]*(1-frac) + sorted[upper]*frac
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/analyzer/stats.go
git commit -m "feat: add statistics calculator with price volatility"
```

---

### Task 6: Add Repository Methods

**Files:**
- Modify: `internal/storage/repository.go`

- [ ] **Step 1: Add instrument stats methods**

```go
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
```

- [ ] **Step 2: Add sync state methods with price direction**

```go
// SyncStateRecord represents sync state
type SyncStateRecord struct {
    ID                  int64
    Coin                string
    DEX                 string
    LastFundingValue    float64
    LastFundingUpdate   time.Time
    FundingUpdateCount  int
    PrevFundingValue    float64
    LastOIUSD           float64
    LastOIUpdate        time.Time
    LastMarkPrice       float64
    LastOraclePrice     float64
    Price30mAgo         float64
    PriceDirection30m   float64
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
```

- [ ] **Step 3: Add signal queue methods**

```go
// SignalQueueRecord represents queued signal
type SignalQueueRecord struct {
    ID                 int64
    Coin               string
    DEX                string
    MarketType         string
    SignalDirection    string
    SignalConfidence   string
    OIChange30m        float64
    OIChange2h         float64
    OIChange24h        float64
    OIUSDCurrent       float64
    FundingCurrent     float64
    FundingChangeAbs   float64
    FundingZScore      float64
    FundingAPRCurrent  float64
    FundingFresh       bool
    PriceChange30m     float64
    PriceChange2h      float64
    MarkPrice          float64
    OraclePrice        float64
    MarkOracleDelta    float64
    CompositeScore     float64
    DetectedAt         time.Time
    DetectedInWindow   string
    Processed          bool
    SentInDigestAt     *time.Time
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

// GetUnprocessedSignalsByDirection retrieves unprocessed signals grouped by direction
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

// MarkSignalsProcessed marks as sent
func (r *Repository) MarkSignalsProcessed(ids []int64) error {
    if len(ids) == 0 {
        return nil
    }
    
    placeholders := make([]string, len(ids))
    args := make([]interface{}, len(ids)+1)
    args[0] = time.Now()
    
    for i, id := range ids {
        placeholders[i] = "?"
        args[i+1] = id
    }
    
    query := fmt.Sprintf(`UPDATE signal_queue SET processed = 1, sent_in_digest_at = ?
        WHERE id IN (%s)`, strings.Join(placeholders, ","))
    
    _, err := r.db.conn.Exec(query, args...)
    return err
}
```

- [ ] **Step 4: Commit**

```bash
git add internal/storage/repository.go
git commit -m "feat: add repository methods for stats, sync state, and signal queue"
```

---

## Phase 4: Multi-Window Analyzer

### Task 7: Create Multi-Window Analysis

**Files:**
- Create: `internal/analyzer/multi_window.go`

- [ ] **Step 1: Create multi-window analyzer with price tracking**

```go
// internal/analyzer/multi_window.go
package analyzer

import (
    "fmt"
    "math"
    "oi_bot_go/internal/storage"
    "time"
)

// MultiWindowAnalysis holds changes across time windows
type MultiWindowAnalysis struct {
    Coin                  string
    DEX                   string
    MarketType            string
    
    // OI changes (%)
    OIChange30m           float64
    OIChange2h            float64
    OIChange24h           float64
    OIUSDCurrent          float64
    OIUSDPrevious         float64
    
    // Funding
    FundingCurrent        float64
    FundingPrevious       float64
    FundingChangePercent  float64
    FundingZScore         float64
    FundingAPR            float64
    FundingFresh          bool
    
    // Price context
    PriceChange30m        float64
    PriceChange2h         float64
    MarkPrice             float64
    OraclePrice           float64
    MarkOracleDelta       float64
    
    // Historical context
    HistoricalFundingMean float64
}

// MultiWindowAnalyzer performs multi-timeframe analysis
type MultiWindowAnalyzer struct {
    repository *storage.Repository
}

// NewMultiWindowAnalyzer creates analyzer
func NewMultiWindowAnalyzer(repo *storage.Repository) *MultiWindowAnalyzer {
    return &MultiWindowAnalyzer{repository: repo}
}

// Analyze performs multi-window analysis
func (mwa *MultiWindowAnalyzer) Analyze(
    coin, dex, marketType string,
    currentOI, currentFunding, currentMarkPrice, currentOraclePrice float64,
    stats *InstrumentStats,
) (*MultiWindowAnalysis, error) {
    
    now := time.Now()
    
    // Get historical OI snapshots
    oi30mAgo, _ := mwa.getOIAtTime(coin, dex, now.Add(-30*time.Minute))
    oi2hAgo, _ := mwa.getOIAtTime(coin, dex, now.Add(-2*time.Hour))
    oi24hAgo, _ := mwa.getOIAtTime(coin, dex, now.Add(-24*time.Hour))
    
    // Get historical price snapshots
    price30mAgo, _ := mwa.getPriceAtTime(coin, dex, now.Add(-30*time.Minute))
    price2hAgo, _ := mwa.getPriceAtTime(coin, dex, now.Add(-2*time.Hour))
    
    analysis := &MultiWindowAnalysis{
        Coin:           coin,
        DEX:            dex,
        MarketType:     marketType,
        OIUSDCurrent:   currentOI,
        FundingCurrent: currentFunding,
        MarkPrice:      currentMarkPrice,
        OraclePrice:    currentOraclePrice,
        FundingAPR:     currentFunding * 24 * 365 * 100,
    }
    
    // Calculate OI changes
    if oi30mAgo > 0 {
        analysis.OIUSDPrevious = oi30mAgo
        analysis.OIChange30m = calculateChangePercent(oi30mAgo, currentOI)
    }
    if oi2hAgo > 0 {
        analysis.OIChange2h = calculateChangePercent(oi2hAgo, currentOI)
    }
    if oi24hAgo > 0 {
        analysis.OIChange24h = calculateChangePercent(oi24hAgo, currentOI)
    }
    
    // Calculate price changes
    if price30mAgo > 0 {
        analysis.PriceChange30m = calculateChangePercent(price30mAgo, currentMarkPrice)
    }
    if price2hAgo > 0 {
        analysis.PriceChange2h = calculateChangePercent(price2hAgo, currentMarkPrice)
    }
    
    // Calculate mark/oracle delta
    if currentOraclePrice > 0 {
        analysis.MarkOracleDelta = (currentMarkPrice - currentOraclePrice) / currentOraclePrice * 100
    }
    
    // Calculate funding Z-score
    if stats != nil && stats.FundingStdDev > 0 {
        analysis.FundingZScore = (currentFunding - stats.FundingMean) / stats.FundingStdDev
        analysis.FundingZScore = math.Max(-5, math.Min(5, analysis.FundingZScore))
        analysis.HistoricalFundingMean = stats.FundingMean
    }
    
    return analysis, nil
}

func (mwa *MultiWindowAnalyzer) getOIAtTime(coin, dex string, targetTime time.Time) (float64, error) {
    query := `SELECT open_interest_usd FROM oi_history 
        WHERE coin = ? AND dex = ? AND timestamp <= ?
        ORDER BY timestamp DESC LIMIT 1`
    
    var oi float64
    err := mwa.repository.GetDB().QueryRow(query, coin, dex, targetTime).Scan(&oi)
    if err != nil {
        return 0, err
    }
    return oi, nil
}

func (mwa *MultiWindowAnalyzer) getPriceAtTime(coin, dex string, targetTime time.Time) (float64, error) {
    query := `SELECT mark_price FROM oi_history 
        WHERE coin = ? AND dex = ? AND timestamp <= ?
        ORDER BY timestamp DESC LIMIT 1`
    
    var price float64
    err := mwa.repository.GetDB().QueryRow(query, coin, dex, targetTime).Scan(&price)
    if err != nil {
        return 0, err
    }
    return price, nil
}

// ShouldAlertInstant checks if triggers instant alert (>30%)
func (mwa *MultiWindowAnalysis) ShouldAlertInstant(threshold float64) bool {
    return math.Abs(mwa.OIChange30m) > threshold ||
           math.Abs(mwa.OIChange2h) > threshold ||
           math.Abs(mwa.FundingZScore) > 3.0 ||
           (mwa.FundingFresh && math.Abs(mwa.FundingChangePercent) > 50.0)
}

// ShouldAlertPeriodic checks if triggers periodic signal
func (mwa *MultiWindowAnalysis) ShouldAlertPeriodic() bool {
    return math.Abs(mwa.OIChange30m) > 15.0 ||
           math.Abs(mwa.OIChange2h) > 20.0 ||
           math.Abs(mwa.OIChange24h) > 30.0 ||
           math.Abs(mwa.FundingZScore) > 2.0 ||
           (mwa.FundingFresh && math.Abs(mwa.FundingChangePercent) > 30.0)
}

func calculateChangePercent(previous, current float64) float64 {
    if previous == 0 {
        return 0
    }
    return ((current - previous) / previous) * 100
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/analyzer/multi_window.go
git commit -m "feat: add multi-window analyzer with price tracking"
```

---

## Phase 5: Signal Scoring & Queue

### Task 8: Create Signal Scorer

**Files:**
- Create: `internal/analyzer/scorer.go`

- [ ] **Step 1: Create momentum-focused scorer**

```go
// internal/analyzer/scorer.go
package analyzer

import "math"

// SignalScorer calculates composite score for momentum signals
type SignalScorer struct{}

// NewSignalScorer creates scorer
func NewSignalScorer() *SignalScorer {
    return &SignalScorer{}
}

// CalculateScore computes momentum-focused score
func (ss *SignalScorer) CalculateScore(analysis *MultiWindowAnalysis) float64 {
    // Speed component (immediate impulse)
    oi30mComponent := math.Min(math.Abs(analysis.OIChange30m)*2, 35)
    
    // Trend component (sustained move)
    oi2hComponent := math.Min(math.Abs(analysis.OIChange2h)*1.5, 25)
    
    // Funding confirmation
    fundingComponent := 0.0
    if analysis.FundingFresh {
        fundingComponent = math.Min(math.Abs(analysis.FundingChangePercent)*0.5, 20)
    } else {
        fundingComponent = math.Min(math.Abs(analysis.FundingZScore)*5, 10)
    }
    
    // Price agreement bonus (when price and OI move together)
    priceAgreement := 0.0
    if (analysis.OIChange30m > 0 && analysis.PriceChange30m > 0) ||
       (analysis.OIChange30m < 0 && analysis.PriceChange30m < 0) {
        priceAgreement = math.Min(math.Abs(analysis.PriceChange30m)*2, 10)
    }
    
    // Size component
    sizeBonus := 0.0
    if analysis.OIUSDCurrent > 0 {
        sizeBonus = math.Min(math.Log10(analysis.OIUSDCurrent/1e6)*2, 10)
        if sizeBonus < 0 {
            sizeBonus = 0
        }
    }
    
    return oi30mComponent + oi2hComponent + fundingComponent + priceAgreement + sizeBonus
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/analyzer/scorer.go
git commit -m "feat: add momentum-focused signal scorer"
```

---

## Phase 6: Message Builder

### Task 9: Create Momentum Digest Builder

**Files:**
- Create: `internal/telegram/digest_builder.go`

- [ ] **Step 1: Create builder for momentum format**

```go
// internal/telegram/digest_builder.go
package telegram

import (
    "fmt"
    "math"
    "oi_bot_go/internal/storage"
    "strings"
    "time"
)

const (
    TelegramLimit = 4096
    SafetyMargin  = 500
    MaxLength     = TelegramLimit - SafetyMargin
)

// DigestBuilder builds momentum-focused digests
type DigestBuilder struct{}

// NewDigestBuilder creates builder
func NewDigestBuilder() *DigestBuilder {
    return &DigestBuilder{}
}

// BuildDigest creates digest by direction (LONG/SHORT/UNCERTAIN)
func (db *DigestBuilder) BuildDigest(longSignals, shortSignals, uncertainSignals []storage.SignalQueueRecord, totalInstruments int) []string {
    if len(longSignals) == 0 && len(shortSignals) == 0 && len(uncertainSignals) == 0 {
        return nil
    }
    
    totalSignals := len(longSignals) + len(shortSignals) + len(uncertainSignals)
    
    var messages []string
    var current strings.Builder
    
    // Header
    header := fmt.Sprintf("🚀 Momentum Signals | %s | %d signals\n\n", time.Now().Format("15:04 UTC"), totalSignals)
    current.WriteString(header)
    
    // Market context
    context := fmt.Sprintf("📊 Market: %d instruments | 🟩 %d | 🟥 %d | ⚪ %d\n\n",
        totalInstruments, len(longSignals), len(shortSignals), len(uncertainSignals))
    current.WriteString(context)
    
    // LONG section
    if len(longSignals) > 0 {
        section := db.formatDirectionSection("🟩 LONG IMPULSE", longSignals)
        if current.Len()+len(section) > MaxLength {
            messages = append(messages, current.String())
            current.Reset()
            current.WriteString("🚀 Signals (continued)...\n\n")
        }
        current.WriteString(section)
    }
    
    // SHORT section
    if len(shortSignals) > 0 {
        section := db.formatDirectionSection("🟥 SHORT IMPULSE", shortSignals)
        if current.Len()+len(section) > MaxLength {
            messages = append(messages, current.String())
            current.Reset()
            current.WriteString("🚀 Signals (continued)...\n\n")
        }
        current.WriteString(section)
    }
    
    // UNCERTAIN section
    if len(uncertainSignals) > 0 {
        section := db.formatDirectionSection("⚪ UNCERTAIN", uncertainSignals)
        if current.Len()+len(section) > MaxLength {
            messages = append(messages, current.String())
            current.Reset()
            current.WriteString("🚀 Signals (continued)...\n\n")
        }
        current.WriteString(section)
    }
    
    // Top movers
    topMovers := db.formatTopMovers(append(append(longSignals, shortSignals...), uncertainSignals...), 5)
    if current.Len()+len(topMovers) > MaxLength {
        messages = append(messages, current.String())
        current.Reset()
        current.WriteString("🚀 Signals (continued)...\n\n")
    }
    current.WriteString(topMovers)
    
    // Footer
    current.WriteString(fmt.Sprintf("\n🕐 Next digest: %s\n", time.Now().Add(30*time.Minute).Format("15:04 UTC")))
    messages = append(messages, current.String())
    
    return messages
}

func (db *DigestBuilder) formatDirectionSection(title string, signals []storage.SignalQueueRecord) string {
    var b strings.Builder
    b.WriteString(title + ":\n")
    b.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
    
    // Group by confidence
    var high, medium, low []storage.SignalQueueRecord
    for _, s := range signals {
        switch s.SignalConfidence {
        case "high":
            high = append(high, s)
        case "medium":
            medium = append(medium, s)
        default:
            low = append(low, s)
        }
    }
    
    if len(high) > 0 {
        b.WriteString("🔥 HIGH CONFIDENCE:\n")
        for _, s := range high {
            b.WriteString(db.formatSignal(s))
        }
        b.WriteString("\n")
    }
    
    if len(medium) > 0 {
        b.WriteString("🟡 MEDIUM CONFIDENCE:\n")
        for _, s := range medium {
            b.WriteString(db.formatSignal(s))
        }
        b.WriteString("\n")
    }
    
    if len(low) > 0 {
        b.WriteString("⚠️ LOW CONFIDENCE:\n")
        for _, s := range low {
            b.WriteString(db.formatSignal(s))
        }
        b.WriteString("\n")
    }
    
    return b.String()
}

func (db *DigestBuilder) formatSignal(s storage.SignalQueueRecord) string {
    freshness := ""
    if s.FundingFresh {
        freshness = " 🆕"
    }
    
    confidenceIcon := ""
    switch s.SignalConfidence {
    case "high":
        confidenceIcon = "🔥"
    case "medium":
        confidenceIcon = "🟡"
    default:
        confidenceIcon = "⚠️"
    }
    
    return fmt.Sprintf(
        "%s %s/%s: OI %s 30m (%s 2h) | Funding %.0f%% APR%s\n"+
        "  └ %s | Price %s | Score: %.0f | %s\n",
        confidenceIcon, s.Coin, s.DEX,
        formatChange(s.OIChange30m), formatChange(s.OIChange2h),
        s.FundingAPRCurrent, freshness,
        formatUSD(s.OIUSDCurrent), formatChange(s.PriceChange30m),
        s.CompositeScore, s.SignalConfidence,
    )
}

func (db *DigestBuilder) formatTopMovers(signals []storage.SignalQueueRecord, n int) string {
    if len(signals) == 0 {
        return ""
    }
    
    // Sort by 24h OI change
    sorted := make([]storage.SignalQueueRecord, len(signals))
    copy(sorted, signals)
    
    for i := 0; i < len(sorted)-1; i++ {
        for j := i + 1; j < len(sorted); j++ {
            if math.Abs(sorted[i].OIChange24h) < math.Abs(sorted[j].OIChange24h) {
                sorted[i], sorted[j] = sorted[j], sorted[i]
            }
        }
    }
    
    var b strings.Builder
    b.WriteString("📈 Top OI Movers (24h):\n")
    
    count := n
    if len(sorted) < count {
        count = len(sorted)
    }
    
    for i := 0; i < count; i++ {
        s := sorted[i]
        direction := "🟩"
        if s.SignalDirection == "short" {
            direction = "🟥"
        } else if s.SignalDirection == "uncertain" {
            direction = "⚪"
        }
        b.WriteString(fmt.Sprintf("%d. %s: %s %s | ", i+1, s.Coin, formatChange(s.OIChange24h), direction))
    }
    b.WriteString("\n\n")
    
    return b.String()
}

// BuildInstantAlert builds instant alert for >30% moves
func (db *DigestBuilder) BuildInstantAlert(s storage.SignalQueueRecord) string {
    direction := "🟩 LONG"
    if s.SignalDirection == "short" {
        direction = "🟥 SHORT"
    }
    
    freshness := ""
    if s.FundingFresh {
        freshness = " 🆕"
    }
    
    return fmt.Sprintf(
        "🚨 CRITICAL MOMENTUM | %s\n\n"+
        "🔥 %s/%s: %s impulse accelerating\n"+
        "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"+
        "OI: %s in window | %s\n"+
        "Funding: %.0f%% APR%s | Z-score: %.1f\n"+
        "Price: %s (mark vs 30m ago)\n\n"+
        "Classification: %s | %s CONFIDENCE\n\n"+
        "Context:\n"+
        "• 30m: %s | 2h: %s | 24h: %s\n"+
        "• Mark/Oracle: %.2f%%\n"+
        "• Signal strength: %.0f/100\n",
        time.Now().Format("15:04 UTC"),
        s.Coin, s.DEX, direction,
        formatChange(s.OIChange30m), formatUSD(s.OIUSDCurrent),
        s.FundingAPRCurrent, freshness, s.FundingZScore,
        formatChange(s.PriceChange30m),
        direction, s.SignalConfidence,
        formatChange(s.OIChange30m), formatChange(s.OIChange2h), formatChange(s.OIChange24h),
        s.MarkOracleDelta,
        s.CompositeScore,
    )
}

func formatChange(change float64) string {
    if change > 0 {
        return fmt.Sprintf("+%.1f%%", change)
    }
    return fmt.Sprintf("%.1f%%", change)
}

func formatUSD(usd float64) string {
    if usd >= 1e9 {
        return fmt.Sprintf("$%.1fB", usd/1e9)
    }
    if usd >= 1e6 {
        return fmt.Sprintf("$%.1fM", usd/1e6)
    }
    return fmt.Sprintf("$%.0fK", usd/1e3)
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/telegram/digest_builder.go
git commit -m "feat: add momentum-focused digest builder with confidence levels"
```

---

## Phase 7: Smart Scheduler Integration

### Task 10: Create MomentumScheduler

**Files:**
- Create: `internal/scheduler/momentum_scheduler.go`

- [ ] **Step 1: Create momentum scheduler**

```go
// internal/scheduler/momentum_scheduler.go
package scheduler

import (
    "context"
    "log"
    "math"
    "oi_bot_go/internal/analyzer"
    "oi_bot_go/internal/hyperliquid"
    "oi_bot_go/internal/storage"
    "oi_bot_go/internal/telegram"
    "strconv"
    "time"
)

// MomentumScheduler implements momentum-based alerting
type MomentumScheduler struct {
    *Scheduler
    
    directionDetector *analyzer.DirectionDetector
    statsCalc         *analyzer.StatsCalculator
    multiAnalyzer     *analyzer.MultiWindowAnalyzer
    scorer            *analyzer.SignalScorer
    digestBuilder     *telegram.DigestBuilder
    
    // Config
    instantThreshold float64
    digestInterval   time.Duration
    nightStartHour   int
    nightEndHour     int
    nightMinScore    float64
    dayMinScore      float64
    
    // Runtime
    lastDigestTime       time.Time
    instantAlertHistory  map[string]time.Time
}

// NewMomentumScheduler creates scheduler
func NewMomentumScheduler(
    client *hyperliquid.Client,
    repository *storage.Repository,
    interval time.Duration,
    telegramBot *telegram.Bot,
) *MomentumScheduler {
    base := NewSchedulerWithOptions(client, repository, interval, 30.0, true)
    base.SetTelegramBot(telegramBot)
    
    return &MomentumScheduler{
        Scheduler:           base,
        directionDetector: analyzer.NewDirectionDetector(),
        statsCalc:           analyzer.NewStatsCalculator(repository),
        multiAnalyzer:       analyzer.NewMultiWindowAnalyzer(repository),
        scorer:              analyzer.NewSignalScorer(),
        digestBuilder:       telegram.NewDigestBuilder(),
        instantThreshold:    30.0,
        digestInterval:      30 * time.Minute,
        nightStartHour:      22,
        nightEndHour:        8,
        nightMinScore:       85,
        dayMinScore:         50,
        instantAlertHistory: make(map[string]time.Time),
    }
}

func (ms *MomentumScheduler) Start(ctx context.Context) error {
    log.Printf("Starting MomentumScheduler (threshold: %.0f%%, digest: %v)",
        ms.instantThreshold, ms.digestInterval)
    
    if ms.telegramBot != nil && ms.telegramBot.IsEnabled() {
        ms.telegramBot.SendAlert("🚀 Momentum OI Monitor started")
    }
    
    if err := ms.collectAndProcess(); err != nil {
        log.Printf("Initial collection failed: %v", err)
    }
    
    collectionTicker := time.NewTicker(ms.interval)
    digestTicker := time.NewTicker(ms.digestInterval)
    defer collectionTicker.Stop()
    defer digestTicker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            log.Println("MomentumScheduler stopped")
            return nil
        case <-collectionTicker.C:
            if err := ms.collectAndProcess(); err != nil {
                log.Printf("Collection failed: %v", err)
            }
        case <-digestTicker.C:
            if err := ms.sendDigest(); err != nil {
                log.Printf("Digest failed: %v", err)
            }
        }
    }
}

func (ms *MomentumScheduler) collectAndProcess() error {
    allData, err := ms.client.GetAllMarketsData()
    if err != nil {
        return err
    }
    
    // Pre-load stats for all instruments
    statsMap := make(map[string]*analyzer.InstrumentStats)
    for _, item := range allData.PerpData {
        stats, err := ms.statsCalc.CalculateStats(item.Coin, item.DEX)
        if err == nil {
            statsMap[item.Coin+"/"+item.DEX] = stats
            ms.repository.SaveInstrumentStats(stats)
        }
    }
    
    // Process each instrument
    for _, item := range allData.PerpData {
        if err := ms.processInstrument(item, statsMap); err != nil {
            log.Printf("Failed to process %s/%s: %v", item.DEX, item.Coin, err)
        }
    }
    
    return nil
}

func (ms *MomentumScheduler) processInstrument(
    item hyperliquid.OpenInterestData,
    statsMap map[string]*analyzer.InstrumentStats,
) error {
    // Parse values
    currentOI, _ := strconv.ParseFloat(item.OpenInterest, 64)
    markPrice, _ := strconv.ParseFloat(item.MarkPrice, 64)
    funding, _ := strconv.ParseFloat(item.Funding, 64)
    
    oiUSD := currentOI * markPrice
    dex := item.DEX
    if dex == "" {
        dex = "native"
    }
    
    // Get stats
    stats := statsMap[item.Coin+"/"+dex]
    
    // Multi-window analysis
    // Note: Need oracle price from API - may need to add to client
    oraclePrice := markPrice // Fallback - ideally get from API
    
    analysis, err := ms.multiAnalyzer.Analyze(
        item.Coin, dex, "perp", oiUSD, funding, markPrice, oraclePrice, stats,
    )
    if err != nil {
        return err
    }
    
    // Detect funding freshness
    fundingFresh := false // Detect based on funding value changes
    
    // Get previous sync state
    prevState, _ := ms.repository.GetSyncState(item.Coin, dex)
    
    if prevState != nil {
        // Check if funding changed
        if math.Abs(funding-prevState.LastFundingValue) > 0.000001 {
            fundingFresh = true
            analysis.FundingPrevious = prevState.LastFundingValue
            analysis.FundingChangePercent = calculateChangePercent(prevState.LastFundingValue, funding)
        }
        analysis.FundingFresh = fundingFresh
    }
    
    // Determine direction
    dirInput := analyzer.DirectionInput{
        OIChange30m:           analysis.OIChange30m,
        PriceChange30m:        analysis.PriceChange30m,
        FundingCurrent:        funding,
        FundingPrevious:       analysis.FundingPrevious,
        FundingFresh:          fundingFresh,
        MarkPrice:             markPrice,
        OraclePrice:           oraclePrice,
        HistoricalFundingMean: 0,
    }
    if stats != nil {
        dirInput.HistoricalFundingMean = stats.FundingMean
    }
    
    dirResult := ms.directionDetector.Detect(dirInput)
    
    // Calculate score
    score := ms.scorer.CalculateScore(analysis)
    
    // Check for instant alert
    if analysis.ShouldAlertInstant(ms.instantThreshold) {
        if ms.shouldSendInstantAlert(item.Coin, dex) {
            signal := &storage.SignalQueueRecord{
                Coin:              item.Coin,
                DEX:               dex,
                SignalDirection:   string(dirResult.Direction),
                SignalConfidence:  string(dirResult.Confidence),
                OIChange30m:       analysis.OIChange30m,
                OIChange2h:        analysis.OIChange2h,
                OIChange24h:       analysis.OIChange24h,
                OIUSDCurrent:      oiUSD,
                FundingCurrent:    funding,
                FundingZScore:     analysis.FundingZScore,
                FundingAPRCurrent: analysis.FundingAPR,
                FundingFresh:      fundingFresh,
                PriceChange30m:    analysis.PriceChange30m,
                MarkPrice:         markPrice,
                OraclePrice:       oraclePrice,
                MarkOracleDelta:   analysis.MarkOracleDelta,
                CompositeScore:    score,
                DetectedInWindow:  "instant",
            }
            ms.sendInstantAlert(signal)
        }
    }
    
    // Check for periodic signal
    if analysis.ShouldAlertPeriodic() && ms.shouldQueueSignal(score) {
        signal := &storage.SignalQueueRecord{
            Coin:              item.Coin,
            DEX:               dex,
            SignalDirection:   string(dirResult.Direction),
            SignalConfidence:  string(dirResult.Confidence),
            OIChange30m:       analysis.OIChange30m,
            OIChange2h:        analysis.OIChange2h,
            OIChange24h:       analysis.OIChange24h,
            OIUSDCurrent:      oiUSD,
            FundingCurrent:    funding,
            FundingZScore:     analysis.FundingZScore,
            FundingAPRCurrent: analysis.FundingAPR,
            FundingFresh:      fundingFresh,
            PriceChange30m:    analysis.PriceChange30m,
            PriceChange2h:     analysis.PriceChange2h,
            MarkPrice:         markPrice,
            OraclePrice:       oraclePrice,
            MarkOracleDelta:   analysis.MarkOracleDelta,
            CompositeScore:    score,
            DetectedInWindow:  "30min",
        }
        ms.repository.SaveSignalToQueue(signal)
    }
    
    // Update sync state
    // ... save state including funding, OI, price
    
    // Save to history
    return ms.repository.SaveOIHistory(item.Coin, dex, "perp", currentOI, markPrice, funding)
}

func (ms *MomentumScheduler) shouldSendInstantAlert(coin, dex string) bool {
    key := coin + "/" + dex
    lastAlert, exists := ms.instantAlertHistory[key]
    if !exists || time.Since(lastAlert) > 15*time.Minute {
        ms.instantAlertHistory[key] = time.Now()
        return true
    }
    return false
}

func (ms *MomentumScheduler) shouldQueueSignal(score float64) bool {
    hour := time.Now().Hour()
    isNight := hour >= ms.nightStartHour || hour < ms.nightEndHour
    
    minScore := ms.dayMinScore
    if isNight {
        minScore = ms.nightMinScore
    }
    
    return score >= minScore
}

func (ms *MomentumScheduler) sendInstantAlert(signal *storage.SignalQueueRecord) {
    if ms.telegramBot == nil || !ms.telegramBot.IsEnabled() {
        return
    }
    
    msg := ms.digestBuilder.BuildInstantAlert(*signal)
    if err := ms.telegramBot.SendAlert(msg); err != nil {
        log.Printf("Failed to send instant alert: %v", err)
    }
}

func (ms *MomentumScheduler) sendDigest() error {
    if ms.telegramBot == nil || !ms.telegramBot.IsEnabled() {
        return nil
    }
    
    // Get signals by direction
    longSignals, _ := ms.repository.GetUnprocessedSignalsByDirection("long", 20)
    shortSignals, _ := ms.repository.GetUnprocessedSignalsByDirection("short", 20)
    uncertainSignals, _ := ms.repository.GetUnprocessedSignalsByDirection("uncertain", 10)
    
    if len(longSignals) == 0 && len(shortSignals) == 0 && len(uncertainSignals) == 0 {
        log.Println("No signals for digest")
        return nil
    }
    
    dexes, _ := ms.repository.GetAllDEXs()
    
    messages := ms.digestBuilder.BuildDigest(longSignals, shortSignals, uncertainSignals, len(dexes)*20)
    
    var allIDs []int64
    for _, s := range longSignals {
        allIDs = append(allIDs, s.ID)
    }
    for _, s := range shortSignals {
        allIDs = append(allIDs, s.ID)
    }
    for _, s := range uncertainSignals {
        allIDs = append(allIDs, s.ID)
    }
    
    for _, msg := range messages {
        if err := ms.telegramBot.SendAlert(msg); err != nil {
            log.Printf("Failed to send digest: %v", err)
        }
    }
    
    return ms.repository.MarkSignalsProcessed(allIDs)
}

// Config setters
func (ms *MomentumScheduler) SetInstantThreshold(t float64) {
    ms.instantThreshold = t
}

func (ms *MomentumScheduler) SetDigestInterval(d time.Duration) {
    ms.digestInterval = d
}

func (ms *MomentumScheduler) SetNightHours(start, end int) {
    ms.nightStartHour = start
    ms.nightEndHour = end
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/scheduler/momentum_scheduler.go
git commit -m "feat: add MomentumScheduler with direction detection and digest"
```

---

## Phase 8: Integration & CLI

### Task 11: Wire Everything in Main

**Files:**
- Modify: `cmd/monitor/main.go`

- [ ] **Step 1: Add CLI flags and wire MomentumScheduler**

```go
// In main(), add new flags:
smartAlerts := flag.Bool("smart-alerts", true, "Enable momentum-based alerting")
instantThreshold := flag.Float64("instant-threshold", 30.0, "Threshold for instant alerts (%)")
digestInterval := flag.Duration("digest-interval", 30*time.Minute, "Digest interval")
nightStart := flag.Int("night-start", 22, "Night mode start hour")
nightEnd := flag.Int("night-end", 8, "Night mode end hour")

// In scheduler initialization:
if *smartAlerts {
    log.Println("Using MomentumScheduler")
    momentumSch := scheduler.NewMomentumScheduler(client, repository, *interval, tgBot)
    momentumSch.SetInstantThreshold(*instantThreshold)
    momentumSch.SetDigestInterval(*digestInterval)
    momentumSch.SetNightHours(*nightStart, *nightEnd)
    sch = momentumSch
} else {
    sch = scheduler.NewSchedulerWithOptions(client, repository, *interval, *alertThreshold, collectAll)
    sch.SetTelegramBot(tgBot)
}
```

- [ ] **Step 2: Commit**

```bash
git add cmd/monitor/main.go
git commit -m "feat: integrate MomentumScheduler with CLI flags"
```

---

## Phase 9: Testing & Documentation

### Task 12: Final Testing

- [ ] **Step 1: Build and test**

```bash
go mod tidy
go build -o oi_monitor ./cmd/monitor

# Test migrations
./oi_monitor -once -debug

# Verify tables
sqlite3 oi_monitor.db ".tables" | grep -E "(instrument_stats|sync_state|signal_queue)"

# Test with debug mode
./oi_monitor -smart-alerts -once -debug
```

- [ ] **Step 2: Update README with momentum strategy**

Add section explaining:
- LONG impulse = OI rising + Funding positive/fresh
- SHORT impulse = OI rising + Funding negative/fresh  
- Fallback to price direction when funding stale
- Confidence levels (HIGH/MEDIUM/LOW)

- [ ] **Step 3: Final commit**

```bash
git add README.md
git commit -m "docs: document momentum strategy and confidence levels"
```

---

*End of Implementation Plan v2.0 — Momentum Strategy*
