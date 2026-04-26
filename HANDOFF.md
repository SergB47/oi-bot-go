# OI Bot Go - Handoff Document (AI Agent Context)

**Purpose:** Complete technical context for AI agents working on this codebase. Includes architecture, algorithms, data flow, and implementation details.

**Last Updated:** 2026-04-26
**Major Version:** Smart Alerting System v2.0 (Momentum Strategy)

---

## 1. Project Overview

OI Bot Go is a Hyperliquid exchange monitoring system that tracks Open Interest (OI) and funding rates across all perpetual DEXes (native + HIP-3). It implements a **momentum-based trading signal detection** strategy to identify insider front-running activity.

### Core Strategy: Front-running Detection
**Hypothesis:** Insiders trade in perps before information becomes public, causing OI to rise while spot price hasn't moved yet. The system trades WITH this impulse.

**Key Insight:** 
- OI rising + Funding positive → More longs opening = 🟩 LONG signal
- OI rising + Funding negative → More shorts opening = 🟥 SHORT signal

---

## 2. Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  Data Collection Layer (5 min intervals)                                      │
│  ├─ Fetch: OI, funding, mark_price, oracle_price from all DEXes             │
│  ├─ Parse string values to float64                                            │
│  └─ Store raw data to oi_history table                                        │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  Statistics Layer (runs on each collection)                                  │
│  ├─ Calculate 14-day rolling stats per instrument (coin+dex)                 │
│  ├─ Compute: funding_mean, funding_stddev, oi_mean, oi_stddev              │
│  ├─ Compute: price_mean, price_volatility                                    │
│  └─ Store to instrument_stats table                                          │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  Direction Detection Layer (per instrument)                                  │
│  ├─ Primary: Fresh funding (>0 and rising = LONG, <0 and falling = SHORT)   │
│  ├─ Fallback: Price direction (OI↑ + Price↑ = LONG, OI↑ + Price↓ = SHORT)  │
│  ├─ Fallback: Mark/Oracle premium (>0.5% = LONG, <-0.5% = SHORT)             │
│  └─ Last resort: Historical funding bias                                     │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  Signal Scoring Layer                                                        │
│  ├─ Multi-window analysis: 30m, 2h, 24h changes                               │
│  ├─ Calculate composite score (0-100)                                       │
│  ├─ Components: OI speed (35pts), OI trend (25pts), funding (20pts)         │
│  ├─ Components: Price agreement (10pts), Size bonus (10pts)                   │
│  └─ Check thresholds: Instant (>30%) vs Periodic (>15%)                      │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  Alert & Digest Layer                                                        │
│  ├─ Instant Mode: Send immediately if >30% OI change or Z-score > 3          │
│  ├─ Periodic Mode: Queue signals, send digest every 30 minutes               │
│  ├─ Night Mode (22:00-08:00): Only score > 85, fresh funding                 │
│  └─ Telegram: Grouped by direction (LONG/SHORT/UNCERTAIN) with confidence    │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 3. Project Structure

```
oi_bot_go/
├── cmd/monitor/
│   └── main.go                    # Entry point, CLI flags, scheduler selection
│
├── internal/
│   ├── analyzer/                  # NEW: Smart alerting components
│   │   ├── types.go              # Direction, Confidence, DirectionInput types
│   │   ├── direction.go          # DirectionDetector (funding + price based)
│   │   ├── direction_test.go     # Unit tests for direction detection
│   │   ├── stats.go              # StatsCalculator (14-day rolling statistics)
│   │   ├── multi_window.go       # MultiWindowAnalyzer (30m/2h/24h windows)
│   │   ├── multi_window_test.go  # Unit tests for multi-window analysis
│   │   └── scorer.go             # SignalScorer (composite score 0-100)
│   │
│   ├── hyperliquid/              # HTTP client for Hyperliquid API
│   │   ├── client.go             # API methods: GetAllMarketsData(), etc.
│   │   └── types.go              # OpenInterestData, PerpDEX, AssetContext
│   │
│   ├── scheduler/                # Data collection schedulers
│   │   ├── scheduler.go          # Legacy Scheduler (simple threshold alerts)
│   │   └── momentum_scheduler.go # NEW: MomentumScheduler (smart alerting)
│   │
│   ├── storage/                  # Database layer
│   │   ├── db.go                 # SQLite connection setup
│   │   ├── migrations.go         # Database schema migrations (v1-v6)
│   │   └── repository.go         # CRUD operations for all tables
│   │
│   └── telegram/                 # Telegram bot
│       ├── bot.go                # Basic Telegram bot
│       └── digest_builder.go     # NEW: Momentum-focused message builder
│
├── docs/superpowers/
│   ├── specs/2026-04-26-smart-alerting-design.md      # Design document
│   └── plans/2026-04-26-smart-alerting-implementation.md # Implementation plan
│
├── go.mod, go.sum               # Dependencies
├── .env.example                  # Environment template
├── README.md                     # User documentation
├── HANDOFF.md                    # This file
└── oi_monitor.db                 # SQLite database (runtime)
```

---

## 4. Database Schema

### 4.1 Core Tables (Existing)

#### oi_history
Primary data storage for OI and funding history.

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER PK | Auto-increment ID |
| coin | TEXT | Asset name (BTC, ETH, xyz:TSLA) |
| dex | TEXT | DEX name (native, xyz, flx, etc.) |
| market_type | TEXT | perp or spot |
| open_interest | REAL | OI in asset units |
| open_interest_usd | REAL | OI in USD (OI * mark_price) |
| mark_price | REAL | Mark price |
| funding | TEXT | Funding rate as string (ex: "-0.0000053132") |
| funding_apr | REAL | Annualized funding rate (%) |
| timestamp | DATETIME | Record timestamp |
| created_at | DATETIME | Creation timestamp |

**Indexes:** idx_oi_history_coin, idx_oi_history_dex, idx_oi_history_timestamp, idx_oi_history_coin_timestamp, idx_oi_history_dex_coin

#### alerts (Legacy)
Simple OI change alerts (used by legacy scheduler).

| Column | Type | Description |
|--------|------|-------------|
| coin, dex, market_type | TEXT | Identifiers |
| previous_oi, current_oi | REAL | OI values |
| change_percent | REAL | Percentage change |
| direction | TEXT | increase or decrease |
| timestamp | DATETIME | Alert time |

#### funding_alerts (Legacy)
Simple funding change alerts.

| Column | Type | Description |
|--------|------|-------------|
| coin, dex | TEXT | Identifiers |
| previous_funding, current_funding | REAL | Funding values |
| change_percent | REAL | Percentage change |
| period_type | TEXT | 5min or 1hour |
| timestamp | DATETIME | Alert time |

### 4.2 Smart Alerting Tables (New)

#### instrument_stats
14-day rolling statistics for each coin+dex combination. Used for Z-score calculation and historical context.

| Column | Type | Description |
|--------|------|-------------|
| coin, dex | TEXT | Primary key (unique) |
| funding_mean_14d | REAL | Mean funding over 14 days |
| funding_stddev_14d | REAL | Standard deviation of funding |
| funding_p50_14d | REAL | 50th percentile (median) funding |
| funding_p95_14d | REAL | 95th percentile funding |
| funding_p5_14d | REAL | 5th percentile funding |
| oi_mean_14d | REAL | Mean OI (USD) over 14 days |
| oi_stddev_14d | REAL | Standard deviation of OI |
| oi_max_14d | REAL | Maximum OI in period |
| oi_min_14d | REAL | Minimum OI in period |
| price_mean_14d | REAL | Mean mark price |
| price_volatility_14d | REAL | Price stddev / mean (CV) |
| last_calculated_at | DATETIME | Last stats update |
| data_points_count | INTEGER | Number of data points used |

**Key Use Cases:**
- Calculate Z-score: `(current_funding - funding_mean_14d) / funding_stddev_14d`
- Determine normal ranges: funding should be within ±2 stddev
- Historical bias: positive funding_mean = bullish bias

#### instrument_sync_state
Tracks last update timestamps and values for detecting funding freshness and price direction.

| Column | Type | Description |
|--------|------|-------------|
| coin, dex | TEXT | Unique identifier |
| last_funding_value | REAL | Last known funding rate |
| last_funding_update | DATETIME | Timestamp of last funding update |
| funding_update_count | INTEGER | Counter of funding updates |
| prev_funding_value | REAL | Previous funding (for change detection) |
| last_oi_usd | REAL | Last OI in USD |
| last_oi_update | DATETIME | Last OI update |
| last_mark_price | REAL | Last mark price |
| last_oracle_price | REAL | Last oracle price |
| price_30m_ago | REAL | Price 30 minutes ago |
| price_direction_30m | REAL | % change in price over 30m |

**Key Use Cases:**
- Detect funding freshness: `now - last_funding_update < 10 minutes`
- Calculate price change: `(mark_price - price_30m_ago) / price_30m_ago * 100`
- Detect funding change: `abs(current_funding - last_funding_value) > epsilon`

#### signal_queue
Temporary queue for signals waiting for periodic digest.

| Column | Type | Description |
|--------|------|-------------|
| coin, dex, market_type | TEXT | Identifiers |
| signal_direction | TEXT | long, short, or uncertain |
| signal_confidence | TEXT | high, medium, or low |
| oi_change_30m, oi_change_2h, oi_change_24h | REAL | OI changes in % |
| oi_usd_current | REAL | Current OI in USD |
| funding_current | REAL | Current funding rate |
| funding_change_abs | REAL | Absolute funding change |
| funding_zscore | REAL | Z-score of funding |
| funding_apr_current | REAL | Current funding APR |
| funding_fresh | BOOLEAN | Is funding fresh (<10min)? |
| price_change_30m, price_change_2h | REAL | Price changes in % |
| mark_price, oracle_price | REAL | Current prices |
| mark_oracle_delta | REAL | (mark - oracle) / oracle * 100 |
| composite_score | REAL | Signal strength 0-100 |
| detected_at | DATETIME | When signal was detected |
| detected_in_window | TEXT | instant, 30min, 2hour, or 24hour |
| processed | BOOLEAN | Has been sent in digest? |
| sent_in_digest_at | DATETIME | When sent |

**Key Use Cases:**
- Queue signals between digests (30 min intervals)
- Group by direction for organized digests
- Filter by confidence level
- Sort by composite_score for priority

---

## 5. Algorithms & Formulas

### 5.1 Direction Detection Algorithm

**Primary Method: Fresh Funding Detection**
```go
// Fresh funding = updated within last 10 minutes
if fundingFresh {
    // Funding positive AND rising = more longs paying = LONG
    if fundingCurrent > 0 && fundingCurrent > fundingPrevious {
        return DirectionLong, ConfidenceHigh
    }
    
    // Funding negative AND falling = more shorts paying = SHORT
    if fundingCurrent < 0 && fundingCurrent < fundingPrevious {
        return DirectionShort, ConfidenceHigh
    }
}
```

**Fallback Method: Price + OI Correlation**
```go
// When funding is stale, use price movement
if !fundingFresh {
    // Strong correlation: OI rising + Price rising = LONG accumulation
    if oiChange30m > 5.0 && priceChange30m > 1.0 {
        return DirectionLong, ConfidenceMedium
    }
    
    // Strong correlation: OI rising + Price falling = SHORT accumulation
    if oiChange30m > 5.0 && priceChange30m < -1.0 {
        return DirectionShort, ConfidenceMedium
    }
    
    // Mark price premium/discount vs oracle
    markOracleDelta = (markPrice - oraclePrice) / oraclePrice * 100
    
    if markOracleDelta > 0.5 && oiChange30m > 5.0 {
        return DirectionLong, ConfidenceMedium
    }
    if markOracleDelta < -0.5 && oiChange30m > 5.0 {
        return DirectionShort, ConfidenceMedium
    }
}
```

**Last Resort: Historical Bias**
```go
// Use 14-day average funding as tiebreaker
if historicalFundingMean > 0 {
    return DirectionLong, ConfidenceLow
} else if historicalFundingMean < 0 {
    return DirectionShort, ConfidenceLow
}
```

### 5.2 Composite Score Algorithm (0-100)

The composite score combines 5 components:

```go
// 1. Speed component (immediate impulse) - max 35 points
oi30mComponent = min(abs(oiChange30m) * 2, 35)

// 2. Trend component (sustained move) - max 25 points
oi2hComponent = min(abs(oiChange2h) * 1.5, 25)

// 3. Funding confirmation - max 20 points
if fundingFresh {
    fundingComponent = min(abs(fundingChangePercent) * 0.5, 20)
} else {
    fundingComponent = min(abs(fundingZScore) * 5, 10)
}

// 4. Price agreement bonus - max 10 points
// Awarded when OI and price move in same direction
if (oiChange30m > 0 && priceChange30m > 0) || 
   (oiChange30m < 0 && priceChange30m < 0) {
    priceAgreement = min(abs(priceChange30m) * 2, 10)
} else {
    priceAgreement = 0
}

// 5. Size bonus (favors larger markets) - max 10 points
sizeBonus = min(log10(oiUSDCurrent / 1e6) * 2, 10)
if sizeBonus < 0 {
    sizeBonus = 0
}

// Total score
totalScore = oi30mComponent + oi2hComponent + fundingComponent + priceAgreement + sizeBonus
```

**Score Interpretation:**
- 90-100: Extreme signal (instant alert always sent)
- 70-89: Strong signal (included in digest, high priority)
- 50-69: Moderate signal (included in digest)
- <50: Weak signal (filtered out unless no other signals)

### 5.3 Alert Triggers

**Instant Alert (immediate Telegram notification):**
```go
instantTrigger = (
    abs(oiChange30m) > 30.0 ||        // OI spike > 30%
    abs(oiChange2h) > 30.0 ||         // OI trend > 30%
    abs(fundingZScore) > 3.0 ||        // Funding anomaly (3 sigma)
    (fundingFresh && abs(fundingChangePercent) > 50.0)  // Funding spike > 50%
)
```

**Periodic Signal (queued for digest):**
```go
periodicTrigger = (
    abs(oiChange30m) > 15.0 ||        // Moderate OI move
    abs(oiChange2h) > 20.0 ||         // Building trend
    abs(oiChange24h) > 30.0 ||        // Strong trend
    abs(fundingZScore) > 2.0 ||        // Funding anomaly
    (fundingFresh && abs(fundingChangePercent) > 30.0)  // Significant funding shift
)
```

### 5.4 Z-Score Calculation

```go
// Z-score measures how many standard deviations from the mean
zScore = (currentValue - mean14d) / stddev14d

// Cap at ±5 to avoid extreme outlier amplification
zScore = max(-5, min(5, zScore))

// Interpretation:
// |zScore| < 1: Normal (within 68% of data)
// 1 < |zScore| < 2: Unusual (within 95% of data)
// 2 < |zScore| < 3: Rare (within 99.7% of data)
// |zScore| > 3: Extreme anomaly (<0.3% probability)
```

### 5.5 Funding APR Calculation

```go
// Hyperliquid pays funding every hour (24 times per day)
// APR = hourly_rate * 24 * 365 * 100
fundingAPR = fundingRate * 24 * 365 * 100

// Example:
// fundingRate = 0.0001 (0.01% per hour)
// APR = 0.0001 * 24 * 365 * 100 = 87.6% per year
```

---

## 6. Component Reference

### 6.1 DirectionDetector (`internal/analyzer/direction.go`)

**Purpose:** Determine LONG vs SHORT direction using multiple detection methods.

**Key Types:**
```go
type Direction string
const (
    DirectionLong       Direction = "long"
    DirectionShort      Direction = "short"
    DirectionUncertain  Direction = "uncertain"
    DirectionUnknown    Direction = "unknown"
)

type SignalConfidence string
const (
    ConfidenceHigh   SignalConfidence = "high"
    ConfidenceMedium SignalConfidence = "medium"
    ConfidenceLow    SignalConfidence = "low"
)

type DirectionInput struct {
    OIChange30m           float64
    PriceChange30m        float64
    FundingCurrent        float64
    FundingPrevious       float64
    FundingFresh          bool
    MarkPrice             float64
    OraclePrice           float64
    HistoricalFundingMean float64
}

type DirectionResult struct {
    Direction  Direction
    Confidence SignalConfidence
    Method     string  // "funding", "price", "historical_bias", "mixed"
}
```

**Usage Example:**
```go
detector := analyzer.NewDirectionDetector()

input := analyzer.DirectionInput{
    OIChange30m:     25.0,
    PriceChange30m:  2.5,
    FundingCurrent:  0.0002,
    FundingPrevious: 0.0001,
    FundingFresh:    true,
    MarkPrice:       50000.0,
    OraclePrice:     49900.0,
}

result := detector.Detect(input)
// result.Direction = DirectionLong
// result.Confidence = ConfidenceHigh
// result.Method = "funding"
```

### 6.2 StatsCalculator (`internal/analyzer/stats.go`)

**Purpose:** Calculate 14-day rolling statistics for Z-score computation.

**Key Methods:**
```go
func NewStatsCalculator(repo *storage.Repository) *StatsCalculator
func (sc *StatsCalculator) CalculateStats(coin, dex string) (*InstrumentStats, error)
```

**Usage Example:**
```go
calc := analyzer.NewStatsCalculator(repository)

stats, err := calc.CalculateStats("BTC", "native")
// Returns: funding_mean, funding_stddev, oi_mean, etc.

// Calculate Z-score
zScore := (currentFunding - stats.FundingMean) / stats.FundingStdDev
```

### 6.3 MultiWindowAnalyzer (`internal/analyzer/multi_window.go`)

**Purpose:** Analyze OI and price changes across multiple time windows.

**Key Types:**
```go
type MultiWindowAnalysis struct {
    Coin                  string
    DEX                   string
    OIChange30m           float64
    OIChange2h            float64
    OIChange24h           float64
    FundingCurrent        float64
    FundingPrevious       float64
    FundingChangePercent  float64
    FundingZScore         float64
    FundingFresh          bool
    PriceChange30m        float64
    PriceChange2h         float64
    MarkPrice             float64
    OraclePrice           float64
    MarkOracleDelta       float64
    HistoricalFundingMean float64
}
```

**Usage Example:**
```go
analyzer := analyzer.NewMultiWindowAnalyzer(repository)

analysis, err := analyzer.Analyze(
    "BTC", "native", "perp",
    currentOI, currentFunding, markPrice, oraclePrice,
    stats,  // *InstrumentStats
)

// Check if should alert
if analysis.ShouldAlertInstant(30.0) {
    // Send instant alert
}
```

### 6.4 SignalScorer (`internal/analyzer/scorer.go`)

**Purpose:** Calculate composite signal strength score (0-100).

**Usage Example:**
```go
scorer := analyzer.NewSignalScorer()

score := scorer.CalculateScore(analysis)
// score is 0-100 based on OI changes, funding, price agreement, size
```

### 6.5 DigestBuilder (`internal/telegram/digest_builder.go`)

**Purpose:** Build formatted Telegram messages with length management.

**Key Methods:**
```go
func NewDigestBuilder() *DigestBuilder
func (db *DigestBuilder) BuildDigest(
    longSignals, shortSignals, uncertainSignals []storage.SignalQueueRecord,
    totalInstruments int,
) []string  // Returns array of messages (split if needed)
func (db *DigestBuilder) BuildInstantAlert(signal storage.SignalQueueRecord) string
```

### 6.6 MomentumScheduler (`internal/scheduler/momentum_scheduler.go`)

**Purpose:** Main scheduler implementing smart alerting with 5min collection + 30min digests.

**Key Configuration:**
```go
type MomentumScheduler struct {
    instantThreshold    float64   // Default: 30.0 (%)
    digestInterval      time.Duration  // Default: 30m
    nightStartHour      int       // Default: 22
    nightEndHour        int       // Default: 8
    nightMinScore       float64   // Default: 85
    dayMinScore         float64   // Default: 50
}
```

**Usage Example:**
```go
scheduler := scheduler.NewMomentumScheduler(
    client, repository, interval, telegramBot,
)

scheduler.SetInstantThreshold(25.0)
scheduler.SetDigestInterval(15 * time.Minute)
scheduler.SetNightHours(23, 7)

ctx := context.Background()
scheduler.Start(ctx)  // Blocks until context cancelled
```

---

## 7. CLI Flags & Configuration

### 7.1 New Smart Alerting Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-smart-alerts` | `true` | Enable momentum-based alerting |
| `-instant-threshold` | `30.0` | OI % threshold for instant alerts |
| `-digest-interval` | `30m` | Interval between digests |
| `-night-start` | `22` | Night mode start hour (24h) |
| `-night-end` | `8` | Night mode end hour (24h) |

### 7.2 Legacy Flags (Still Supported)

| Flag | Default | Description |
|------|---------|-------------|
| `-db` | `oi_monitor.db` | Database path |
| `-interval` | `5m` | Collection interval |
| `-threshold` | `20.0` | Legacy OI alert threshold (when smart-alerts=false) |
| `-once` | - | One-shot mode |
| `-native` | - | Only native DEX |
| `-debug` | - | Debug mode (no Telegram) |
| `-history` | - | Show coin history |

### 7.3 Environment Variables (.env)

```bash
TELEGRAM_BOT_TOKEN=your_bot_token_here
TELEGRAM_CHAT_ID=your_chat_id_here
```

---

## 8. Common Tasks for AI Agents

### 8.1 Add New Detection Method

1. Add method to `DirectionDetector` in `direction.go`
2. Update `Detect()` to call new method in appropriate priority
3. Add test case in `direction_test.go`
4. Update confidence calculation if needed

### 8.2 Modify Scoring Algorithm

1. Edit `CalculateScore()` in `scorer.go`
2. Adjust component weights (currently: 35/25/20/10/10)
3. Add new component if needed
4. Update tests in `scorer_test.go`
5. Update README score documentation

### 8.3 Add New Database Table

1. Add migration function in `migrations.go` (migrateVX)
2. Add to migration list in `RunMigrations()`
3. Add repository methods in `repository.go`
4. Test migration: `go run ./cmd/monitor -once -debug`

### 8.4 Debug Signal Issues

```sql
-- Check recent signals for a coin
SELECT * FROM signal_queue 
WHERE coin = 'BTC' AND dex = 'native'
ORDER BY detected_at DESC LIMIT 10;

-- Check stats calculation
SELECT * FROM instrument_stats 
WHERE coin = 'BTC' AND dex = 'native';

-- Check sync state (funding freshness)
SELECT coin, last_funding_update, 
       (strftime('%s', 'now') - strftime('%s', last_funding_update)) / 60 as minutes_old
FROM instrument_sync_state
WHERE coin = 'BTC';
```

---

## 9. Testing & Validation

### 9.1 Run All Tests
```bash
go test ./internal/analyzer/... -v    # Direction, multi-window, scorer tests
go test ./internal/storage/... -v     # Repository tests
go test ./...                         # All tests
```

### 9.2 Manual Testing
```bash
# Build and run one-shot with debug
./oi_monitor -once -debug

# Check database
cat oi_monitor.db | sqlite3 ".tables"
sqlite3 oi_monitor.db "SELECT * FROM signal_queue WHERE processed = 0;"
```

### 9.3 Test Specific Component
```bash
# Run specific test
go test ./internal/analyzer -run TestDirectionDetector -v
```

---

## 10. Troubleshooting Guide

### Issue: No signals being generated
**Check:**
1. Is data being collected? Check `oi_history` table has recent entries
2. Are stats calculated? Check `instrument_stats` has entries
3. Is threshold too high? Lower `-instant-threshold` or `-day-min-score`

### Issue: Wrong direction detection
**Check:**
1. Is funding being detected as fresh? Check `instrument_sync_state.funding_update_count`
2. Is price data available? Check `mark_price` and `oracle_price` in `oi_history`
3. Add debug logging to `DirectionDetector.Detect()`

### Issue: Telegram messages too long
**Check:**
1. `DigestBuilder.BuildDigest()` should split messages at 3500 chars
2. If still failing, reduce `MaxSignalsPerDigest` constant
3. Consider enabling message splitting in Telegram bot

### Issue: Night mode not filtering
**Check:**
1. Verify system timezone or use UTC consistently
2. Check `shouldQueueSignal()` logic in `momentum_scheduler.go`
3. Verify `nightMinScore` is being applied

---

## 11. External Resources

- **Design Document:** `docs/superpowers/specs/2026-04-26-smart-alerting-design.md`
- **Implementation Plan:** `docs/superpowers/plans/2026-04-26-smart-alerting-implementation.md`
- **Hyperliquid API Docs:** https://hyperliquid.gitbook.io/hyperliquid-docs/
- **Telegram Bot API:** https://core.telegram.org/bots/api
- **Go SQLite Driver:** https://pkg.go.dev/modernc.org/sqlite

---

*This document is maintained for AI agent context transfer. For user documentation, see README.md*
