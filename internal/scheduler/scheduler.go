package scheduler

import (
	"context"
	"fmt"
	"log"
	"math"
	"strconv"
	"time"

	"oi_bot_go/internal/hyperliquid"
	"oi_bot_go/internal/storage"
	"oi_bot_go/internal/telegram"
)

// Scheduler handles periodic data collection and alerting
type Scheduler struct {
	client              *hyperliquid.Client
	repository          *storage.Repository
	telegramBot         *telegram.Bot
	interval            time.Duration
	oiAlertThreshold    float64  // Percentage threshold for OI alerts
	fundingAlert5Min    float64  // Threshold for 5-min funding alerts (default 20%)
	fundingAlert1Hour   float64  // Threshold for 1-hour funding alerts (default 10%)
	collectAll          bool     // Whether to collect from all DEXes
}

// NewScheduler creates a new scheduler instance
func NewScheduler(client *hyperliquid.Client, repository *storage.Repository, interval time.Duration, alertThreshold float64) *Scheduler {
	return &Scheduler{
		client:           client,
		repository:       repository,
		interval:         interval,
		oiAlertThreshold: alertThreshold,
		fundingAlert5Min: 20.0,  // Default 20% for 5 min
		fundingAlert1Hour: 10.0, // Default 10% for 1 hour
		collectAll:       true,
	}
}

// NewSchedulerWithOptions creates a scheduler with custom options
func NewSchedulerWithOptions(client *hyperliquid.Client, repository *storage.Repository, 
	interval time.Duration, alertThreshold float64, collectAll bool) *Scheduler {
	return &Scheduler{
		client:           client,
		repository:       repository,
		interval:         interval,
		oiAlertThreshold: alertThreshold,
		fundingAlert5Min: 20.0,
		fundingAlert1Hour: 10.0,
		collectAll:       collectAll,
	}
}

// SetTelegramBot sets the Telegram bot for alerts
func (s *Scheduler) SetTelegramBot(bot *telegram.Bot) {
	s.telegramBot = bot
}

// Start begins the scheduling loop
func (s *Scheduler) Start(ctx context.Context) error {
	log.Printf("Starting scheduler (interval: %v, OI threshold: %.2f%%, funding 5min: %.2f%%, funding 1h: %.2f%%)",
		s.interval, s.oiAlertThreshold, s.fundingAlert5Min, s.fundingAlert1Hour)

	if s.telegramBot != nil && s.telegramBot.IsEnabled() {
		s.telegramBot.SendAlert("🚀 OI Monitor started")
	}

	// Do initial collection immediately
	if err := s.collectAndProcess(); err != nil {
		log.Printf("Initial collection failed: %v", err)
	}

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Scheduler stopped")
			return nil
		case <-ticker.C:
			if err := s.collectAndProcess(); err != nil {
				log.Printf("Collection failed: %v", err)
			}
		}
	}
}

// collectAndProcess fetches data and processes all assets
func (s *Scheduler) collectAndProcess() error {
	log.Printf("Collecting data at %s...", time.Now().Format("2006-01-02 15:04:05"))

	if s.collectAll {
		return s.collectAllMarkets()
	}
	return s.collectNativeOnly()
}

// collectAllMarkets collects from all DEXes
func (s *Scheduler) collectAllMarkets() error {
	allData, err := s.client.GetAllMarketsData()
	if err != nil {
		return fmt.Errorf("failed to fetch markets data: %w", err)
	}

	totalAssets := len(allData.PerpData)
	log.Printf("Fetched data for %d assets", totalAssets)

	// Process perpetual data
	for _, item := range allData.PerpData {
		if err := s.processAsset(item); err != nil {
			log.Printf("Failed to process %s/%s: %v", item.DEX, item.Coin, err)
			continue
		}
	}

	log.Printf("Data collection completed")
	return nil
}

// collectNativeOnly collects only from native DEX
func (s *Scheduler) collectNativeOnly() error {
	data, err := s.client.GetOpenInterest()
	if err != nil {
		return fmt.Errorf("failed to fetch OI data: %w", err)
	}

	log.Printf("Fetched data for %d assets from native DEX", len(data))

	for _, item := range data {
		if err := s.processAsset(item); err != nil {
			log.Printf("Failed to process %s: %v", item.Coin, err)
			continue
		}
	}

	return nil
}

// processAsset processes a single asset
func (s *Scheduler) processAsset(item hyperliquid.OpenInterestData) error {
	// Parse current values
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

	dex := item.DEX
	if dex == "" {
		dex = "native"
	}

	marketType := item.MarketType
	if marketType == "" {
		marketType = "perp"
	}

	// Get last record for OI alerts
	lastRecord, err := s.repository.GetLastOI(item.Coin, dex)
	if err != nil {
		return fmt.Errorf("failed to get last OI: %w", err)
	}

	// Check for OI change alert
	if lastRecord != nil && lastRecord.OpenInterest > 0 {
		changePercent := calculateChangePercent(lastRecord.OpenInterest, currentOI)
		if math.Abs(changePercent) >= s.oiAlertThreshold {
			direction := "increase"
			if changePercent < 0 {
				direction = "decrease"
			}

			if err := s.repository.SaveAlert(item.Coin, dex, marketType,
				lastRecord.OpenInterest, currentOI, changePercent, direction); err != nil {
				log.Printf("Failed to save OI alert for %s/%s: %v", dex, item.Coin, err)
			} else {
				log.Printf("OI ALERT: [%s/%s] %s OI %s by %.2f%% (%.4f -> %.4f)",
					dex, marketType, item.Coin, direction, math.Abs(changePercent),
					lastRecord.OpenInterest, currentOI)
			}
		}
	}

	// Check for funding rate changes
	if lastRecord != nil && lastRecord.Funding != "" {
		oldFunding, _ := strconv.ParseFloat(lastRecord.Funding, 64)
		s.checkFundingAlerts(item.Coin, dex, oldFunding, funding)
	}

	// Save current data
	if err := s.repository.SaveOIHistory(item.Coin, dex, marketType, currentOI, markPrice, funding); err != nil {
		return fmt.Errorf("failed to save OI history: %w", err)
	}

	return nil
}

// checkFundingAlerts checks for significant funding rate changes
func (s *Scheduler) checkFundingAlerts(coin, dex string, oldFunding, newFunding float64) {
	// Skip if funding is 0 (likely delisted or no data)
	if oldFunding == 0 || newFunding == 0 {
		return
	}

	now := time.Now()

	// Check 5-minute change
	funding5MinAgo, _, err := s.repository.GetFundingAtTime(coin, dex, now.Add(-5*time.Minute))
	if err == nil && funding5MinAgo != 0 {
		change5Min := calculateChangePercent(funding5MinAgo, newFunding)
		if math.Abs(change5Min) >= s.fundingAlert5Min {
			s.triggerFundingAlert(coin, dex, funding5MinAgo, newFunding, change5Min, "5min")
		}
	}

	// Check 1-hour change
	funding1HourAgo, _, err := s.repository.GetFundingAtTime(coin, dex, now.Add(-1*time.Hour))
	if err == nil && funding1HourAgo != 0 {
		change1Hour := calculateChangePercent(funding1HourAgo, newFunding)
		if math.Abs(change1Hour) >= s.fundingAlert1Hour {
			s.triggerFundingAlert(coin, dex, funding1HourAgo, newFunding, change1Hour, "1hour")
		}
	}
}

// triggerFundingAlert saves and sends a funding alert
func (s *Scheduler) triggerFundingAlert(coin, dex string, oldFunding, newFunding, changePercent float64, period string) {
	// Save to database
	if err := s.repository.SaveFundingAlert(coin, dex, oldFunding, newFunding, changePercent, period); err != nil {
		log.Printf("Failed to save funding alert for %s/%s: %v", dex, coin, err)
		return
	}

	log.Printf("FUNDING ALERT: [%s/%s] changed by %.2f%% over %s (%.8f -> %.8f)",
		dex, coin, math.Abs(changePercent), period, oldFunding, newFunding)

	// Send Telegram notification
	if s.telegramBot != nil && s.telegramBot.IsEnabled() {
		if err := s.telegramBot.SendFundingAlert(coin, dex, oldFunding, newFunding, changePercent, period); err != nil {
			log.Printf("Failed to send Telegram alert: %v", err)
		}
	}
}

// calculateChangePercent calculates percentage change
func calculateChangePercent(previous, current float64) float64 {
	if previous == 0 {
		return 0
	}
	return ((current - previous) / previous) * 100
}

// CollectOnce performs a single collection
func (s *Scheduler) CollectOnce() error {
	return s.collectAndProcess()
}

// CollectOnceAllMarkets performs a single collection from all markets
func (s *Scheduler) CollectOnceAllMarkets() error {
	return s.collectAllMarkets()
}
