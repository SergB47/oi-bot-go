package scheduler

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math"
	"oi_bot_go/internal/analyzer"
	"oi_bot_go/internal/hyperliquid"
	"oi_bot_go/internal/storage"
	"oi_bot_go/internal/telegram"
	"sort"
	"strconv"
	"sync"
	"time"
)

// MomentumScheduler implements momentum-based alerting with direction detection
type MomentumScheduler struct {
	*Scheduler

	directionDetector *analyzer.DirectionDetector
	statsCalc         *analyzer.StatsCalculator
	multiAnalyzer     *analyzer.MultiWindowAnalyzer
	scorer            *analyzer.SignalScorer
	digestBuilder     *telegram.DigestBuilder

	// Configuration
	instantThreshold float64
	digestInterval   time.Duration
	nightStartHour   int
	nightEndHour     int
	nightMinScore    float64
	dayMinScore      float64

	// Runtime state
	lastDigestTime          time.Time
	instantAlertHistory     map[string]time.Time
	instantAlertHistoryMu   sync.RWMutex
}

// NewMomentumScheduler creates a new momentum-based scheduler
func NewMomentumScheduler(
	client *hyperliquid.Client,
	repository *storage.Repository,
	interval time.Duration,
	telegramBot *telegram.Bot,
) *MomentumScheduler {
	// Create base scheduler with options
	base := NewSchedulerWithOptions(client, repository, interval, 30.0, true)
	base.SetTelegramBot(telegramBot)

	// Create history provider for stats calculator
	historyProvider := func(coin, dex string, from, to time.Time) ([]analyzer.HistoryRecord, error) {
		records, err := repository.GetOIHistoryForCoinAndDEX(coin, dex, from, to)
		if err != nil {
			return nil, err
		}
		var result []analyzer.HistoryRecord
		for _, r := range records {
			result = append(result, analyzer.HistoryRecord{
				Coin:            r.Coin,
				DEX:             r.DEX,
				OpenInterestUSD: r.OpenInterestUSD,
				MarkPrice:       r.MarkPrice,
				Funding:         r.Funding,
				Timestamp:       r.Timestamp,
			})
		}
		return result, nil
	}

	// Create OI and price providers for multi-window analyzer
	oiProvider := func(coin, dex string, targetTime time.Time) (float64, error) {
		query := `SELECT open_interest_usd FROM oi_history 
			WHERE coin = ? AND dex = ? AND timestamp <= ?
			ORDER BY timestamp DESC LIMIT 1`
		var oi float64
		err := repository.GetDB().QueryRow(query, coin, dex, targetTime).Scan(&oi)
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return oi, err
	}

	priceProvider := func(coin, dex string, targetTime time.Time) (float64, error) {
		query := `SELECT mark_price FROM oi_history 
			WHERE coin = ? AND dex = ? AND timestamp <= ?
			ORDER BY timestamp DESC LIMIT 1`
		var price float64
		err := repository.GetDB().QueryRow(query, coin, dex, targetTime).Scan(&price)
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return price, err
	}

	return &MomentumScheduler{
		Scheduler:           base,
		directionDetector:   analyzer.NewDirectionDetector(),
		statsCalc:           analyzer.NewStatsCalculator(historyProvider),
		multiAnalyzer:       analyzer.NewMultiWindowAnalyzer(oiProvider, priceProvider),
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

// Start begins the momentum scheduling loop with collection and digest tickers
func (ms *MomentumScheduler) Start(ctx context.Context) error {
	log.Printf("Starting MomentumScheduler (threshold: %.0f%%, digest: %v, night: %d:00-%d:00)",
		ms.instantThreshold, ms.digestInterval, ms.nightStartHour, ms.nightEndHour)

	if ms.telegramBot != nil && ms.telegramBot.IsEnabled() {
		ms.telegramBot.SendAlert("🚀 Momentum OI Monitor started")
	}

	// Do initial collection immediately
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

// collectAndProcess fetches data, calculates stats, and processes all instruments
func (ms *MomentumScheduler) collectAndProcess() error {
	allData, err := ms.client.GetAllMarketsData()
	if err != nil {
		return fmt.Errorf("failed to fetch markets data: %w", err)
	}

	// Pre-load stats for all instruments
	statsMap := make(map[string]*analyzer.InstrumentStats)
	for _, item := range allData.PerpData {
		dex := item.DEX
		if dex == "" {
			dex = "native"
		}
		stats, err := ms.statsCalc.CalculateStats(item.Coin, dex)
		if err == nil {
			statsMap[item.Coin+"/"+dex] = stats
			if err := ms.repository.SaveInstrumentStats(stats); err != nil {
				log.Printf("Failed to save stats for %s/%s: %v", item.Coin, dex, err)
			}
		}
	}

	// Collect all instant signals first, then send top 5
	var instantSignals []*storage.SignalQueueRecord

	// Process each instrument
	for _, item := range allData.PerpData {
		signal, err := ms.processInstrument(item, statsMap)
		if err != nil {
			log.Printf("Failed to process %s/%s: %v", item.DEX, item.Coin, err)
		}
		if signal != nil {
			instantSignals = append(instantSignals, signal)
		}
	}

	// Send top 5 instant alerts sorted by score (only if score >= 70)
	if len(instantSignals) > 0 {
		ms.sendTopInstantAlerts(instantSignals)
	}

	return nil
}

// processInstrument analyzes a single instrument and returns signal if it meets instant criteria
func (ms *MomentumScheduler) processInstrument(
	item hyperliquid.OpenInterestData,
	statsMap map[string]*analyzer.InstrumentStats,
) (*storage.SignalQueueRecord, error) {
	// Parse values
	currentOI, err := strconv.ParseFloat(item.OpenInterest, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse OI: %w", err)
	}

	markPrice, err := strconv.ParseFloat(item.MarkPrice, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse mark price: %w", err)
	}

	funding := 0.0
	if item.Funding != "" {
		funding, _ = strconv.ParseFloat(item.Funding, 64)
	}

	oiUSD := currentOI * markPrice
	dex := item.DEX
	if dex == "" {
		dex = "native"
	}

	// Get previous state for freshness detection
	prevState, _ := ms.repository.GetSyncState(item.Coin, dex)

	// Detect funding freshness by checking if funding changed
	fundingFresh := false
	if prevState != nil {
		if math.Abs(funding-prevState.LastFundingValue) > 0.000001 {
			fundingFresh = true
		}
	}

	// Multi-window analysis
	analysis, err := ms.multiAnalyzer.Analyze(
		item.Coin, dex, "perp",
		oiUSD, funding, markPrice, markPrice, // oracle price not available, using mark
		statsMap[item.Coin+"/"+dex],
	)
	if err != nil {
		return nil, fmt.Errorf("multi-window analysis failed: %w", err)
	}

	// Set funding freshness and previous values
	analysis.FundingFresh = fundingFresh
	if prevState != nil {
		analysis.FundingPrevious = prevState.LastFundingValue
		if fundingFresh {
			analysis.FundingChangePercent = calculateChangePercent(prevState.LastFundingValue, funding)
		}
	}

	// Determine direction using DirectionDetector
	dirInput := analyzer.DirectionInput{
		OIChange30m:           analysis.OIChange30m,
		PriceChange30m:        analysis.PriceChange30m,
		FundingCurrent:        funding,
		FundingPrevious:       analysis.FundingPrevious,
		FundingFresh:          fundingFresh,
		MarkPrice:             markPrice,
		OraclePrice:           markPrice,
		HistoricalFundingMean: 0,
	}
	if stats := statsMap[item.Coin+"/"+dex]; stats != nil {
		dirInput.HistoricalFundingMean = stats.FundingMean
	}

	dirResult := ms.directionDetector.Detect(dirInput)

	// Calculate composite score
	score := ms.scorer.CalculateScore(analysis)

	// Check if this instrument qualifies for instant alert
	// STRICT criteria: only truly significant moves
	isSignificant := false

	// Criteria 1: Large OI change (>10% in any window)
	if math.Abs(analysis.OIChange30m) >= 10.0 || math.Abs(analysis.OIChange2h) >= 10.0 || math.Abs(analysis.OIChange24h) >= 15.0 {
		isSignificant = true
	}

	// Criteria 2: Extreme funding anomaly (Z-score > 3.0) AND meaningful OI
	// Require both Z-score AND some OI change to avoid noise
	if math.Abs(analysis.FundingZScore) >= 3.0 && (math.Abs(analysis.OIChange30m) >= 5.0 || math.Abs(analysis.OIChange2h) >= 5.0) {
		isSignificant = true
	}

	// Criteria 3: Strong momentum signal (score >= 60)
	if score >= 60 {
		isSignificant = true
	}

	// Must have historical data AND minimum score of 60
	if prevState == nil || score < 60 {
		isSignificant = false
	}

	var instantSignal *storage.SignalQueueRecord

	// Check for instant alert (>30% moves)
	if analysis.ShouldAlertInstant(ms.instantThreshold) && isSignificant {
		if ms.shouldSendInstantAlert(item.Coin, dex) {
			instantSignal = &storage.SignalQueueRecord{
				Coin:              item.Coin,
				DEX:               dex,
				MarketType:        "perp",
				SignalDirection:   string(dirResult.Direction),
				SignalConfidence:  string(dirResult.Confidence),
				OIChange30m:       analysis.OIChange30m,
				OIChange2h:        analysis.OIChange2h,
				OIChange24h:       analysis.OIChange24h,
				OIUSDCurrent:      oiUSD,
				FundingCurrent:    funding,
				FundingChangeAbs:  math.Abs(analysis.FundingChangePercent),
				FundingZScore:     analysis.FundingZScore,
				FundingAPRCurrent: analysis.FundingAPR,
				FundingFresh:      fundingFresh,
				PriceChange30m:    analysis.PriceChange30m,
				PriceChange2h:     analysis.PriceChange2h,
				MarkPrice:         markPrice,
				OraclePrice:       markPrice,
				MarkOracleDelta:   analysis.MarkOracleDelta,
				CompositeScore:    score,
				DetectedInWindow:  "instant",
			}
		}
	}

	// Check for periodic signal (queued for digest)
	if analysis.ShouldAlertPeriodic() && ms.shouldQueueSignal(score) {
		signal := &storage.SignalQueueRecord{
			Coin:              item.Coin,
			DEX:               dex,
			MarketType:        "perp",
			SignalDirection:   string(dirResult.Direction),
			SignalConfidence:  string(dirResult.Confidence),
			OIChange30m:       analysis.OIChange30m,
			OIChange2h:        analysis.OIChange2h,
			OIChange24h:       analysis.OIChange24h,
			OIUSDCurrent:      oiUSD,
			FundingCurrent:    funding,
			FundingChangeAbs:  math.Abs(analysis.FundingChangePercent),
			FundingZScore:     analysis.FundingZScore,
			FundingAPRCurrent: analysis.FundingAPR,
			FundingFresh:      fundingFresh,
			PriceChange30m:    analysis.PriceChange30m,
			PriceChange2h:     analysis.PriceChange2h,
			MarkPrice:         markPrice,
			OraclePrice:       markPrice,
			MarkOracleDelta:   analysis.MarkOracleDelta,
			CompositeScore:    score,
			DetectedInWindow:  "30min",
		}
		if err := ms.repository.SaveSignalToQueue(signal); err != nil {
			log.Printf("Failed to queue signal for %s/%s: %v", item.Coin, dex, err)
		}
	}

	// Update sync state
	now := time.Now()
	newState := &analyzer.SyncState{
		Coin:               item.Coin,
		DEX:                dex,
		LastFundingValue:   funding,
		LastFundingUpdate:  now,
		LastOIUSD:          oiUSD,
		LastOIUpdate:       now,
		LastMarkPrice:      markPrice,
		LastOraclePrice:    markPrice,
		Price30mAgo:        markPrice,
		PriceDirection30m:  analysis.PriceChange30m,
	}
	if prevState != nil {
		newState.FundingUpdateCount = prevState.FundingUpdateCount
		if fundingFresh {
			newState.FundingUpdateCount++
			newState.PrevFundingValue = prevState.LastFundingValue
		}
	}

	if err := ms.repository.SaveSyncState(newState); err != nil {
		log.Printf("Failed to save sync state for %s/%s: %v", item.Coin, dex, err)
	}

	// Save to OI history
	if err := ms.repository.SaveOIHistory(item.Coin, dex, "perp", currentOI, markPrice, funding); err != nil {
		return instantSignal, fmt.Errorf("failed to save OI history: %w", err)
	}

	return instantSignal, nil
}

// sendTopInstantAlerts sends top signals by score in a single message (max 5)
func (ms *MomentumScheduler) sendTopInstantAlerts(signals []*storage.SignalQueueRecord) {
	if len(signals) == 0 {
		return
	}

	// Filter only signals with score >= 60
	var significantSignals []*storage.SignalQueueRecord
	for _, s := range signals {
		if s.CompositeScore >= 60 {
			significantSignals = append(significantSignals, s)
		}
	}

	// If no significant signals, don't send
	if len(significantSignals) == 0 {
		log.Printf("No significant instant signals (all below score 60), skipping batch alert")
		return
	}

	// Sort by score descending
	sort.Slice(significantSignals, func(i, j int) bool {
		return significantSignals[i].CompositeScore > significantSignals[j].CompositeScore
	})

	// Take top 5 (or fewer if less available)
	topCount := 5
	if len(significantSignals) < topCount {
		topCount = len(significantSignals)
	}
	topSignals := significantSignals[:topCount]

	// Send combined alert
	if ms.telegramBot != nil && ms.telegramBot.IsEnabled() {
		msg := ms.digestBuilder.BuildInstantAlertBatch(topSignals)
		if err := ms.telegramBot.SendAlert(msg); err != nil {
			log.Printf("Failed to send instant alert batch: %v", err)
		}
	}
}

// shouldSendInstantAlert checks rate limiting (1 per 15min per instrument)
func (ms *MomentumScheduler) shouldSendInstantAlert(coin, dex string) bool {
	key := coin + "/" + dex

	ms.instantAlertHistoryMu.RLock()
	lastAlert, exists := ms.instantAlertHistory[key]
	ms.instantAlertHistoryMu.RUnlock()

	if !exists || time.Since(lastAlert) > 15*time.Minute {
		ms.instantAlertHistoryMu.Lock()
		defer ms.instantAlertHistoryMu.Unlock()

		// Double-check after acquiring write lock
		lastAlert, exists = ms.instantAlertHistory[key]
		if !exists || time.Since(lastAlert) > 15*time.Minute {
			ms.instantAlertHistory[key] = time.Now()
			return true
		}
	}
	return false
}

// shouldQueueSignal checks if signal should be queued based on night mode filtering
func (ms *MomentumScheduler) shouldQueueSignal(score float64) bool {
	hour := time.Now().Hour()
	isNight := hour >= ms.nightStartHour || hour < ms.nightEndHour

	minScore := ms.dayMinScore
	if isNight {
		minScore = ms.nightMinScore
	}

	return score >= minScore
}

// sendDigest sends periodic digest of all queued signals
func (ms *MomentumScheduler) sendDigest() error {
	if ms.telegramBot == nil || !ms.telegramBot.IsEnabled() {
		return nil
	}

	// Get signals by direction
	longSignals, err := ms.repository.GetUnprocessedSignalsByDirection("long", 20)
	if err != nil {
		return fmt.Errorf("failed to get long signals: %w", err)
	}

	shortSignals, err := ms.repository.GetUnprocessedSignalsByDirection("short", 20)
	if err != nil {
		return fmt.Errorf("failed to get short signals: %w", err)
	}

	uncertainSignals, err := ms.repository.GetUnprocessedSignalsByDirection("uncertain", 10)
	if err != nil {
		return fmt.Errorf("failed to get uncertain signals: %w", err)
	}

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

// SetInstantThreshold sets the threshold for instant alerts
func (ms *MomentumScheduler) SetInstantThreshold(t float64) {
	ms.instantThreshold = t
}

// SetDigestInterval sets the digest interval
func (ms *MomentumScheduler) SetDigestInterval(d time.Duration) {
	ms.digestInterval = d
}

// SetNightHours sets night mode hours
func (ms *MomentumScheduler) SetNightHours(start, end int) {
	ms.nightStartHour = start
	ms.nightEndHour = end
}

// SetMinScores sets minimum scores for day and night modes
func (ms *MomentumScheduler) SetMinScores(dayScore, nightScore float64) {
	ms.dayMinScore = dayScore
	ms.nightMinScore = nightScore
}

// CollectOnce performs a single collection cycle (for -once flag)
// Overrides base Scheduler.CollectOnce to use momentum logic instead of legacy alerts
func (ms *MomentumScheduler) CollectOnce() error {
	return ms.collectAndProcess()
}

// CollectOnceAllMarkets performs a single collection from all markets (for -once -all flags)
// Overrides base Scheduler.CollectOnceAllMarkets to use momentum logic
func (ms *MomentumScheduler) CollectOnceAllMarkets() error {
	return ms.collectAndProcess()
}
