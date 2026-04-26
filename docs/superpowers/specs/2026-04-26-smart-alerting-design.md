# Smart Alerting System — Design Document

**Date:** 2026-04-26  
**Project:** OI Bot Go — Hyperliquid OI & Funding Monitor  
**Status:** Draft

---

## 1. Goal

Transform the current simple threshold-based alerting system into an intelligent **momentum trading signal generator** that:
- Detects when insiders are front-running information via perps before spot price moves
- Uses hybrid detection (fixed thresholds + statistical deviations per instrument)
- Sends consolidated digest notifications instead of spam
- Handles Telegram message limits gracefully
- Accounts for the different update frequencies of OI (continuous) and funding (hourly)
- **NEW:** Determines direction (LONG/SHORT) even when funding is stale

---

## 2. Strategy Overview

### 2.1 Momentum Strategy (Front-running Detection)

**Core Hypothesis:** Insiders trade in perps before information becomes public, causing OI to rise while the spot price hasn't moved yet. We trade WITH this impulse, not against it.

**Signal Logic:**

| Condition | Signal | Reasoning |
|-----------|--------|-----------|
| OI ↑ + Funding ↑ (positive and increasing) | 🟩 **LONG** | More longs opening, willing to pay premium = bullish front-running |
| OI ↑ + Funding ↓ (negative and decreasing) | 🟥 **SHORT** | More shorts opening, willing to pay premium = bearish front-running |
| OI ↑ + Funding stale/flat + Price ↑ | 🟩 **LONG** | Price rising with OI = long accumulation |
| OI ↑ + Funding stale/flat + Price ↓ | 🟥 **SHORT** | Price falling with OI = short accumulation |

**Funding as Confirmation:**
- Positive funding (> 0) = Longs pay shorts = more long positions = confirm LONG
- Negative funding (< 0) = Shorts pay longs = more short positions = confirm SHORT
- Stale funding (> 50 min old) = Use price direction as proxy

### 2.2 Why This Works

On Hyperliquid:
- Normal funding: ~10% APR (slightly positive in bull markets)
- When insiders accumulate LONG: Funding rises (20%, 50%, 100% APR) + OI explodes
- When insiders accumulate SHORT: Funding goes deeply negative (-20%, -50% APR) + OI explodes
- **Before funding updates:** We can detect direction via mark price movement relative to oracle

---

## 3. Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Data Collection Layer (5 min)                        │
│  - Fetch OI, funding, mark price, oracle price from all DEXes                 │
│  - Store to oi_history table                                                 │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                       Statistics Calculator (5 min)                          │
│  - Calculate rolling 14-day stats per instrument (coin+dex)                 │
│  - Update Z-scores for OI and funding                                        │
│  - Store to instrument_stats table                                           │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Alert Detection Layer                                │
│                                                                              │
│  ┌─────────────────────┐  ┌─────────────────────────────────────────────────┐ │
│  │  INSTANT MODE       │  │  PERIODIC MODE (30 min cycle)                   │ │
│  │  Trigger:           │  │  Trigger: Every 30 min timer                   │ │
│  │  - Change > 30% OR  │  │  Analysis windows:                              │ │
│  │  - Z-score > 3      │  │  - 30 min (immediate context)                  │ │
│  │  Action: Immediate  │  │  - 2 hours (medium-term trend)                 │ │
│  │  Telegram alert     │  │  - 24 hours (long-term context)                │ │
│  │  (always sent)      │  │  Action: Queue signals, send digest if any     │ │
│  └─────────────────────┘  └─────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Signal Processing Layer                                │
│                                                                              │
│  1. Direction Detection:                                                      │
│     IF funding fresh (< 10 min):                                             │
│       - LONG:  Funding > 0 and rising                                       │
│       - SHORT: Funding < 0 and falling                                      │
│     ELSE (funding stale):                                                     │
│       - LONG:  Price ↑ + OI ↑ (mark > oracle)                                │
│       - SHORT: Price ↓ + OI ↑ (mark < oracle)                                │
│                                                                              │
│  2. Classification:                                                            │
│     - 🟩 LONG IMPULSE:  OI ↑ + Direction = LONG                             │
│     - 🟥 SHORT IMPULSE: OI ↑ + Direction = SHORT                            │
│     - ⚪ UNCERTAIN:     OI ↑ + Direction unclear                            │
│                                                                              │
│  3. Scoring (composite signal strength):                                     │
│     Score = |OI_change_30m| × 0.4 + |OI_change_2h| × 0.3 +                  │
│             |Funding_change| × 0.2 + log10(OI_USD) × 0.1                    │
│                                                                              │
│  4. Confidence Level:                                                        │
│     - HIGH:   Fresh funding + direction confirmed                           │
│     - MEDIUM: Stale funding + price direction agrees with historical funding │
│     - LOW:    Stale funding + conflicting signals                           │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                        Message Builder & Sender                              │
│                                                                              │
│  - Format: Trading-signal grouped by direction (LONG/SHORT/UNCERTAIN)       │
│  - Check length < 3500 chars (Telegram limit 4096 with margin)               │
│  - If overflow: split into multiple messages with "(continued)"            │
│  - If storm (>20 signals): show top by score, add "+N more" footer          │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 4. Data Model Extensions

### 4.1 New Table: `instrument_stats`

Stores rolling 14-day statistics for each instrument (coin + DEX combination).

```sql
CREATE TABLE instrument_stats (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    coin TEXT NOT NULL,
    dex TEXT NOT NULL DEFAULT 'native',
    
    -- Funding statistics (14-day rolling)
    funding_mean_14d REAL DEFAULT 0,
    funding_stddev_14d REAL DEFAULT 0,
    funding_p50_14d REAL DEFAULT 0,
    funding_p95_14d REAL DEFAULT 0,
    funding_p5_14d REAL DEFAULT 0,
    
    -- OI statistics (14-day rolling, in USD)
    oi_mean_14d REAL DEFAULT 0,
    oi_stddev_14d REAL DEFAULT 0,
    oi_max_14d REAL DEFAULT 0,
    oi_min_14d REAL DEFAULT 0,
    
    -- Price statistics
    price_mean_14d REAL DEFAULT 0,
    price_volatility_14d REAL DEFAULT 0,
    
    -- Update tracking
    last_calculated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    data_points_count INTEGER DEFAULT 0,
    
    UNIQUE(coin, dex)
);

CREATE INDEX idx_instrument_stats_coin_dex ON instrument_stats(coin, dex);
CREATE INDEX idx_instrument_stats_last_calc ON instrument_stats(last_calculated_at);
```

### 4.2 New Table: `instrument_sync_state`

Tracks last update timestamps and last known values.

```sql
CREATE TABLE instrument_sync_state (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    coin TEXT NOT NULL,
    dex TEXT NOT NULL DEFAULT 'native',
    
    -- Funding state
    last_funding_value REAL DEFAULT 0,
    last_funding_update DATETIME,
    funding_update_count INTEGER DEFAULT 0,
    prev_funding_value REAL DEFAULT 0,  -- For detecting change
    
    -- OI state  
    last_oi_usd REAL DEFAULT 0,
    last_oi_update DATETIME,
    
    -- Price state (for direction detection)
    last_mark_price REAL DEFAULT 0,
    last_oracle_price REAL DEFAULT 0,
    price_30m_ago REAL DEFAULT 0,
    price_direction_30m REAL DEFAULT 0,  -- % change
    
    UNIQUE(coin, dex)
);

CREATE INDEX idx_sync_state_coin_dex ON instrument_sync_state(coin, dex);
```

### 4.3 New Table: `signal_queue`

Temporary queue for signals waiting for periodic digest.

```sql
CREATE TABLE signal_queue (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    coin TEXT NOT NULL,
    dex TEXT NOT NULL,
    market_type TEXT DEFAULT 'perp',
    
    -- Signal classification (UPDATED for momentum strategy)
    signal_direction TEXT CHECK(signal_direction IN ('long', 'short', 'uncertain')),
    signal_confidence TEXT CHECK(signal_confidence IN ('high', 'medium', 'low')),
    
    -- OI changes (%)
    oi_change_30m REAL DEFAULT 0,
    oi_change_2h REAL DEFAULT 0,
    oi_change_24h REAL DEFAULT 0,
    oi_usd_current REAL DEFAULT 0,
    
    -- Funding changes
    funding_current REAL DEFAULT 0,
    funding_change_abs REAL DEFAULT 0,
    funding_zscore REAL DEFAULT 0,
    funding_apr_current REAL DEFAULT 0,
    funding_fresh BOOLEAN DEFAULT 0,
    
    -- Price context for direction detection
    price_change_30m REAL DEFAULT 0,
    price_change_2h REAL DEFAULT 0,
    mark_price REAL DEFAULT 0,
    oracle_price REAL DEFAULT 0,
    mark_oracle_delta REAL DEFAULT 0,  -- (mark - oracle) / oracle * 100
    
    -- Signal strength
    composite_score REAL DEFAULT 0,
    
    -- Timestamps
    detected_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    detected_in_window TEXT CHECK(detected_in_window IN ('instant', '30min', '2hour', '24hour')),
    
    -- Processing state
    processed BOOLEAN DEFAULT 0,
    sent_in_digest_at DATETIME,
    
    UNIQUE(coin, dex, detected_at)
);

CREATE INDEX idx_signal_queue_unprocessed ON signal_queue(processed, composite_score DESC);
CREATE INDEX idx_signal_queue_detected ON signal_queue(detected_at);
CREATE INDEX idx_signal_queue_direction ON signal_queue(signal_direction, processed);
```

---

## 5. Direction Detection Algorithm

### 5.1 Primary Method: Fresh Funding

```go
func detectDirectionByFunding(currentFunding, prevFunding float64, fundingFresh bool) Direction {
    if !fundingFresh {
        return DirectionUnknown
    }
    
    // Funding rising and positive = more longs paying
    if currentFunding > 0 && currentFunding > prevFunding {
        return DirectionLong
    }
    
    // Funding falling and negative = more shorts paying  
    if currentFunding < 0 && currentFunding < prevFunding {
        return DirectionShort
    }
    
    // Funding positive but stable/falling = uncertain
    // Funding negative but stable/rising = uncertain
    return DirectionUncertain
}
```

### 5.2 Fallback Method: Price + OI Correlation

When funding is stale (> 10 min old), use price movement:

```go
func detectDirectionByPrice(
    oiChange30m float64,
    priceChange30m float64,
    markOracleDelta float64,
    historicalFundingBias float64,  // mean funding over 14 days
) Direction {
    // Strong correlation: OI rising + Price rising = LONG accumulation
    if oiChange30m > 5.0 && priceChange30m > 1.0 {
        return DirectionLong
    }
    
    // Strong correlation: OI rising + Price falling = SHORT accumulation
    if oiChange30m > 5.0 && priceChange30m < -1.0 {
        return DirectionShort
    }
    
    // Mark price vs Oracle (premium/discount)
    // Positive premium = mark > oracle = longs pushing price up
    if markOracleDelta > 0.5 && oiChange30m > 5.0 {
        return DirectionLong
    }
    
    // Negative premium = mark < oracle = shorts pushing price down
    if markOracleDelta < -0.5 && oiChange30m > 5.0 {
        return DirectionShort
    }
    
    // Use historical bias as tiebreaker
    if historicalFundingBias > 0 {
        return DirectionLong
    } else if historicalFundingBias < 0 {
        return DirectionShort
    }
    
    return DirectionUncertain
}
```

### 5.3 Confidence Level

```go
type SignalConfidence string

const (
    ConfidenceHigh   SignalConfidence = "high"
    ConfidenceMedium SignalConfidence = "medium"  
    ConfidenceLow    SignalConfidence = "low"
)

func calculateConfidence(
    fundingFresh bool,
    fundingDirection Direction,
    priceDirection Direction,
    historicalFundingBias float64,
) SignalConfidence {
    if fundingFresh {
        // Fresh funding is authoritative
        if fundingDirection != DirectionUncertain {
            return ConfidenceHigh
        }
        // Fresh but unclear funding - use price as medium confidence
        if priceDirection != DirectionUncertain {
            return ConfidenceMedium
        }
        return ConfidenceLow
    }
    
    // Stale funding - rely on price
    if priceDirection != DirectionUncertain {
        // Check if price agrees with historical bias
        if (priceDirection == DirectionLong && historicalFundingBias > 0) ||
           (priceDirection == DirectionShort && historicalFundingBias < 0) {
            return ConfidenceMedium
        }
        return ConfidenceLow
    }
    
    return ConfidenceLow
}
```

---

## 6. Detection Algorithms

### 6.1 Instant Alert Trigger (>30% moves)

```go
instantTrigger = (
    abs(oiChange30m) > 30.0 ||
    abs(oiChange2h) > 30.0 ||
    abs(fundingZScore) > 3.0 ||
    (fundingFresh && abs(fundingChangePercent) > 50.0)  // 50% funding spike
)
```

### 6.2 Periodic Analysis (30 min cycle)

```go
shouldSignal = (
    abs(oiChange30m) > 15.0 ||      // Moderate OI move
    abs(oiChange2h) > 20.0 ||       // Building trend
    abs(oiChange24h) > 30.0 ||      // Strong trend
    abs(fundingZScore) > 2.0 ||     // Funding anomaly
    (fundingFresh && abs(fundingChange) > 30.0)  // Significant funding shift
)
```

### 6.3 Signal Classification (Momentum Strategy)

```go
func classifyMomentumSignal(analysis *MultiWindowAnalysis) (SignalDirection, SignalConfidence) {
    // Primary: Try funding-based detection
    fundingDir := detectDirectionByFunding(
        analysis.FundingCurrent, 
        analysis.FundingPrevious,
        analysis.FundingFresh,
    )
    
    // Fallback: Price-based detection
    priceDir := detectDirectionByPrice(
        analysis.OIChange30m,
        analysis.PriceChange30m,
        analysis.MarkOracleDelta,
        analysis.HistoricalFundingMean,
    )
    
    // Determine final direction
    direction := DirectionUncertain
    if fundingDir != DirectionUnknown {
        direction = fundingDir
    } else if priceDir != DirectionUncertain {
        direction = priceDir
    }
    
    // Calculate confidence
    confidence := calculateConfidence(
        analysis.FundingFresh,
        fundingDir,
        priceDir,
        analysis.HistoricalFundingMean,
    )
    
    return direction, confidence
}
```

### 6.4 Composite Score (Momentum-Focused)

```go
func calculateMomentumScore(analysis *MultiWindowAnalysis) float64 {
    // Speed component (immediate impulse)
    oi30mComponent := min(abs(analysis.OIChange30m) * 2, 35)      // max 35
    
    // Trend component (sustained move)
    oi2hComponent := min(abs(analysis.OIChange2h) * 1.5, 25)    // max 25
    
    // Funding confirmation (stronger if fresh and directional)
    fundingComponent := 0.0
    if analysis.FundingFresh {
        fundingComponent = min(abs(analysis.FundingChangePercent) * 0.5, 20)  // max 20
    } else {
        fundingComponent = min(abs(analysis.FundingZScore) * 5, 10)  // max 10 if stale
    }
    
    // Price agreement bonus (when price and OI move together)
    priceAgreement := 0.0
    if (analysis.OIChange30m > 0 && analysis.PriceChange30m > 0) ||
       (analysis.OIChange30m < 0 && analysis.PriceChange30m < 0) {
        priceAgreement = min(abs(analysis.PriceChange30m) * 2, 10)  // max 10
    }
    
    // Size component
    sizeBonus := 0.0
    if analysis.OIUSDCurrent > 0 {
        sizeBonus = min(log10(analysis.OIUSDCurrent/1e6) * 2, 10)
        if sizeBonus < 0 {
            sizeBonus = 0
        }
    }
    
    return oi30mComponent + oi2hComponent + fundingComponent + priceAgreement + sizeBonus
}
```

---

## 7. Message Format (Momentum Strategy)

### 7.1 Standard Digest Format

```
🚀 Momentum Signals | 14:30 UTC | 12 signals detected

📊 Market Context:
Active: 47 instruments | LONG bias: 8 | SHORT bias: 4
Avg funding: 12% APR (slightly bullish)

🟩 LONG IMPULSE (Front-running detected):
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔥 HIGH CONFIDENCE:
• BTC/native: OI +35% 30m (+42% 2h) | Funding +28% APR 🆕
  └ $2.1B→$2.8B | Price +2.1% | Score: 97
  └ Signal: Fresh funding + price agreement
  
• ETH/native: OI +28% 30m | Funding +42% APR
  └ $1.5B→$1.9B | Price +1.8% | Score: 85

🟡 MEDIUM CONFIDENCE:
• SOL/native: OI +22% 30m (+24% 2h)
  └ $890M→$1.1B | Price +1.5% | Score: 72
  └ Signal: Stale funding (23m), price confirms LONG

🟥 SHORT IMPULSE (Bearish accumulation):
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔥 HIGH CONFIDENCE:
• DOGE/native: OI +45% 30m | Funding -35% APR 🆕
  └ $120M→$174M | Price -3.2% | Score: 92
  └ Signal: Fresh negative funding + price falling

🟡 MEDIUM CONFIDENCE:
• TSLA/xyz: OI +18% 30m
  └ $45M→$53M | Price -1.8% | Score: 68
  └ Signal: Stale funding, price/oi correlation

⚪ UNCERTAIN (OI moving, direction unclear):
• GOLD/xyz: OI +25% | Funding flat | Price flat
  └ Score: 55 | Needs more data

📈 Top OI Movers (24h):
1. DOGE: +68% 🟥  2. BTC: +55% 🟩  3. ETH: +42% 🟩

🕐 Next digest: 15:00 UTC
```

### 7.2 Instant Alert Format (Critical >30% moves)

```
🚨 CRITICAL MOMENTUM | 14:32 UTC

🔥 BTC/native: LONG impulse accelerating
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
OI: +35% in 5 min ($2.1B → $2.8B)
Funding: +15% → +28% APR 🆕 (just updated!)
Price: +2.4% (mark) vs +2.1% (oracle)

Classification: 🟩 LONG IMPULSE | HIGH CONFIDENCE

Context:
• 30m:  +35% | 2h:  +42% | 24h:  +68%
• Funding Z-score: +3.2σ (extreme bullish)
• Mark/Oracle premium: +0.3% (longs paying up)
• Historical funding bias: +8% APR (bullish)

Signal strength: 97/100
Confidence: HIGH (fresh funding + price agreement)

💡 Interpretation: Strong insider accumulation detected
   in perps before expected spot price move.
```

### 7.3 Funding Freshness Indicators

- 🆕 = Updated < 10 min ago (fresh, high confidence)
- ⏱️ = Updated 10-30 min ago (recent, medium confidence)
- ⏳ = Updated 30-50 min ago (getting stale, verify with price)
- ⚠️ = Updated > 50 min ago (stale, rely on price direction only)

---

## 8. Night Mode Logic

```go
type NightModeConfig struct {
    StartHour        int     // 22 (10 PM)
    EndHour          int     // 8 (8 AM)
    
    // Only signals above these thresholds sent during night
    OIThreshold      float64 // 50% (vs 20% day) - only extreme moves
    MinScore         float64 // 85 (vs 50 day) - only strong signals
    RequireFreshFunding bool // true (only high confidence at night)
}

func isNightMode(cfg NightModeConfig) bool {
    hour := time.Now().Hour()
    return hour >= cfg.StartHour || hour < cfg.EndHour
}

func shouldSendDuringNight(signal Signal, cfg NightModeConfig) bool {
    // Night mode: only extreme signals with high confidence
    if signal.CompositeScore < cfg.MinScore {
        return false
    }
    if abs(signal.OIChange30m) < cfg.OIThreshold {
        return false
    }
    if cfg.RequireFreshFunding && signal.Confidence != ConfidenceHigh {
        return false
    }
    return true
}
```

---

## 9. Configuration

### 9.1 New CLI Flags

```bash
./oi_monitor \
    -smart-alerts              # Enable smart alerting system \
    -instant-threshold 30.0     # % change for instant alert \
    -digest-interval 30m       # Periodic digest interval \
    -night-start 22            # Night mode start hour \
    -night-end 8               # Night mode end hour \
    -stats-window 14d          # Historical window for statistics \
    -min-signal-score 50       # Minimum score to include in digest \
    -funding-normal-apr 10.0   # Normal funding APR baseline for bias calc
```

### 9.2 Environment Variables

```bash
# In .env
SMART_ALERTS_ENABLED=true
INSTANT_THRESHOLD=30.0
DIGEST_INTERVAL_MINUTES=30
NIGHT_MODE_START=22
NIGHT_MODE_END=8
NIGHT_MIN_SCORE=85
DAY_MIN_SCORE=50
FUNDING_NORMAL_APR=10.0
```

---

## 10. Success Criteria

1. **Direction Detection:** Correctly identifies LONG vs SHORT with >70% accuracy when funding fresh
2. **Fallback Works:** When funding stale, price-based detection provides usable direction
3. **No Spam:** Single digest replaces 10-50 individual alerts
4. **Night Safety:** Only extreme signals (score > 85) sent during 22:00-08:00
5. **Telegram Safe:** No "Message too long" errors, graceful splitting
6. **Insider Detection:** High-score signals (>90) correlate with subsequent 1-4h price moves in predicted direction

---

## Appendix A: Example Scenarios

### Scenario 1: Insider Long Accumulation (The "Gold" Signal)
```
14:00   BTC: OI=$100M, Funding=+10% APR, Price=$50,000
14:05   BTC: OI=$103M (+3%), Funding=+10%, Price=$50,100 (+0.2%)
        → Not triggered (change too small)
        
14:30   BTC: OI=$135M (+35% in 30min), Funding=+28% APR 🆕, Price=$51,000 (+2%)
        → 🚨 INSTANT ALERT triggered
        → Direction: LONG (funding positive and rising)
        → Confidence: HIGH (fresh funding, price agrees)
        → Score: 97
        
Result: User gets early warning of insider accumulation
```

### Scenario 2: Stale Funding + Price Direction
```
14:00   ETH: Funding last updated, OI=$500M, Price=$3,000
14:30   ETH: OI=$580M (+16% in 30min), Funding=stale (45min old), Price=$3,060 (+2%)
        → Periodic signal triggered
        → Direction: LONG (OI rising + price rising)
        → Confidence: MEDIUM (funding stale but price confirms)
        → Score: 72
        
Result: User gets signal even without fresh funding
```

### Scenario 3: Funding Flip (Short Squeeze Incoming)
```
13:00   DOGE: OI=$100M, Funding=-15% APR (shorts paying)
14:00   DOGE: OI=$120M, Funding=-15%
...
14:05   DOGE: OI=$145M (+45% in 5min), Funding=-35% APR 🆕 (more negative!)
        → 🚨 INSTANT ALERT
        → Direction: SHORT (funding negative and falling = more shorts)
        → Wait... this is actually MORE shorts opening!
        → Score: 92
        
        [But wait - this is either:]
        A) Aggressive shorting (bearish) - if price falling
        B) Short squeeze setup (bullish) - if price rising
        
        Price: $0.12 → $0.125 (+4.2%)
        → Price rising with shorts opening = SHORT SQUEEZE
        → Reclassify: Actually BULLISH (squeeze coming)
```

---

*End of Design Document v2.0 — Momentum Strategy*
