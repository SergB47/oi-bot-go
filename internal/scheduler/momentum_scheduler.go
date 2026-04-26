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
		directionDetector: analyzer.NewDirectionDetector(),
		statsCalc:           analyzer.NewStatsCalculator(historyProvider),
		multiAnalyzer:       analyzer.NewMultiWindowAnalyzer(oiProvider, priceProvider),
		scorer:              analyzer.NewSignalScorer(),
		digestBuilder:       telegram.NewDigestBuilder(),
		instantThreshold:    30.0,
		digestInterval:    30 * time.Minute,
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

	// Process each instrument
	for _, item := range allData.PerpData {
		if err := ms.processInstrument(item, statsMap); err != nil {
			log.Printf("Failed to process %s/%s: %v", item.DEX, item.Coin, err)
		}
	}

	return nil
}

// processInstrument analyzes a single instrument and triggers alerts/signals
func (ms *MomentumScheduler) processInstrument(
	item hyperliquid.OpenInterestData,
	statsMap map[string]*analyzer.InstrumentStats,
) error {
	// Parse values
	currentOI, err := strconv.ParseFloat(item.OpenInterest, 64)
	if err != nil {
		return fmt.Errorf("failed to parse OI: %w", err)
	}

	markPrice, err := strconv.ParseFloat(item.MarkPrice, 64)
	if err != nil {
		return fmt.Errorf("failed to parse mark price: %w", err)
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

	// Get stats for this instrument
	stats := statsMap[item.Coin+"/"+dex]

	// Get oracle price (using mark price as fallback since API may not provide it directly)
	oraclePrice := markPrice

	// Get previous sync state for funding freshness detection
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
		oiUSD, funding, markPrice, oraclePrice,
		stats,
	)
	if err != nil {
		return fmt.Errorf("multi-window analysis failed: %w", err)
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
		OraclePrice:           oraclePrice,
		HistoricalFundingMean: 0,
	}
	if stats != nil {
		dirInput.HistoricalFundingMean = stats.FundingMean
	}

	dirResult := ms.directionDetector.Detect(dirInput)

	// Calculate composite score
	score := ms.scorer.CalculateScore(analysis)

	// Check for instant alert (>30% moves)
	// Only send if we have historical data (prevState exists) and meaningful changes
	if analysis.ShouldAlertInstant(ms.instantThreshold) && prevState != nil {
		// Validate we have actual historical data, not just zeros
		hasHistoricalData := math.Abs(analysis.OIChange30m) > 0.1 || math.Abs(analysis.OIChange2h) > 0.1
		if !hasHistoricalData {
			log.Printf("Skipping instant alert for %s/%s: insufficient historical data (OI changes are zero)", dex, item.Coin)
		} else if ms.shouldSendInstantAlert(item.Coin, dex) {
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
			OraclePrice:       oraclePrice,
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
		LastOraclePrice:    oraclePrice,
		Price30mAgo:        analysis.OIUSDPrevious,
		PriceDirection30m:  analysis.PriceChange30m,
	}
	if prevState != nil {
		newState.FundingUpdateCount = prevState.FundingUpdateCount
		newState.PrevFundingValue = prevState.LastFundingValue
		if fundingFresh {
			newState.FundingUpdateCount++
		}
	}
	if err := ms.repository.SaveSyncState(newState); err != nil {
		log.Printf("Failed to save sync state for %s/%s: %v", item.Coin, dex, err)
	}

	// Save to OI history
	return ms.repository.SaveOIHistory(item.Coin, dex, "perp", currentOI, markPrice, funding)
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

// sendInstantAlert sends an instant alert via Telegram
func (ms *MomentumScheduler) sendInstantAlert(signal *storage.SignalQueueRecord) {
	if ms.telegramBot == nil || !ms.telegramBot.IsEnabled() {
		return
	}

	msg := ms.digestBuilder.BuildInstantAlert(*signal)
	if err := ms.telegramBot.SendAlert(msg); err != nil {
		log.Printf("Failed to send instant alert: %v", err)
	} else {
		log.Printf("Instant alert sent for %s/%s (score: %.0f)", signal.Coin, signal.DEX, signal.CompositeScore)
	}
}

// sendDigest sends periodic digest of all queued signals
func (ms *MomentumScheduler) sendDigest() error {
	if ms.telegramBot == nil || !ms.telegramBot.IsEnabled() {
		return nil
	}

	// Get unprocessed signals by direction
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

	// Count total instruments
	dexes, _ := ms.repository.GetAllDEXs()
	totalInstruments := len(dexes) * 20 // Approximate

	// Build digest messages
	messages := ms.digestBuilder.BuildDigest(longSignals, shortSignals, uncertainSignals, totalInstruments)

	// Collect all signal IDs for marking as processed
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

	// Send messages
	for _, msg := range messages {
		if err := ms.telegramBot.SendAlert(msg); err != nil {
			log.Printf("Failed to send digest: %v", err)
		}
	}

	// Mark signals as processed
	if err := ms.repository.MarkSignalsProcessed(allIDs); err != nil {
		return fmt.Errorf("failed to mark signals processed: %w", err)
	}

	ms.lastDigestTime = time.Now()
	log.Printf("Digest sent with %d signals", len(allIDs))
	return nil
}

// SetInstantThreshold sets the threshold for instant alerts
func (ms *MomentumScheduler) SetInstantThreshold(t float64) {
	ms.instantThreshold = t
}

// SetDigestInterval sets the interval between digests
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
