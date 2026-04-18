package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"oi_bot_go/internal/hyperliquid"
	"oi_bot_go/internal/scheduler"
	"oi_bot_go/internal/storage"
	"oi_bot_go/internal/telegram"
)

const (
	defaultDBPath          = "oi_monitor.db"
	defaultInterval        = 5 * time.Minute
	defaultAlertThreshold  = 20.0 // OI change threshold
	defaultFunding5Min     = 20.0 // Funding change 5 min threshold
	defaultFunding1Hour    = 10.0 // Funding change 1 hour threshold
)

func main() {
	// Load .env file if exists
	_ = godotenv.Load()

	// Parse command line flags
	dbPath := flag.String("db", defaultDBPath, "Path to SQLite database file")
	interval := flag.Duration("interval", defaultInterval, "Data collection interval (e.g., 5m, 1h)")
	alertThreshold := flag.Float64("threshold", defaultAlertThreshold, "OI alert threshold percentage")
	_ = flag.Float64("funding-5min", defaultFunding5Min, "Funding change alert threshold for 5 min")
	_ = flag.Float64("funding-1hour", defaultFunding1Hour, "Funding change alert threshold for 1 hour")
	oneShot := flag.Bool("once", false, "Run data collection once and exit")
	allMarkets := flag.Bool("all", true, "Collect from all DEXes")
	nativeOnly := flag.Bool("native", false, "Collect only from native DEX")
	debugMode := flag.Bool("debug", false, "Debug mode: skip Telegram bot initialization")
	showHistory := flag.String("history", "", "Show OI history for specified coin")
	showDEXHistoryFlag := flag.String("dex-history", "", "Show OI history for specified DEX")
	showAlerts := flag.Bool("alerts", false, "Show recent OI alerts")
	showFundingAlerts := flag.Bool("funding-alerts", false, "Show recent funding alerts")
	listDEXs := flag.Bool("list-dexes", false, "List all DEXs in database")
	flag.Parse()

	// Initialize database
	log.Printf("Initializing database: %s", *dbPath)
	db, err := storage.NewDB(*dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	repository := storage.NewRepository(db)

	// Handle CLI commands
	if *showHistory != "" {
		showCoinHistory(repository, *showHistory)
		return
	}
	if *showDEXHistoryFlag != "" {
		showDEXHistory(repository, *showDEXHistoryFlag)
		return
	}
	if *showAlerts {
		showRecentAlerts(repository)
		return
	}
	if *showFundingAlerts {
		showRecentFundingAlerts(repository)
		return
	}
	if *listDEXs {
		listAllDEXs(repository)
		return
	}

	// Initialize Hyperliquid client
	client := hyperliquid.NewClient()

	// Initialize Telegram bot (skip in debug mode)
	var tgBot *telegram.Bot
	if !*debugMode {
		botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
		chatIDStr := os.Getenv("TELEGRAM_CHAT_ID")
		
		if botToken != "" && chatIDStr != "" {
			var chatID int64
			if _, err := fmt.Sscanf(chatIDStr, "%d", &chatID); err == nil {
				tgBot, err = telegram.NewBot(botToken, chatID)
				if err != nil {
					log.Printf("Failed to create Telegram bot: %v", err)
				} else {
					log.Println("Telegram bot initialized successfully")
				}
			} else {
				log.Printf("Invalid TELEGRAM_CHAT_ID: %s", chatIDStr)
			}
		} else {
			log.Println("Telegram bot not configured (set TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID in .env)")
		}
	} else {
		log.Println("Debug mode: Telegram bot disabled")
	}

	// Determine collection mode
	collectAll := *allMarkets && !*nativeOnly

	// Initialize scheduler
	sch := scheduler.NewSchedulerWithOptions(client, repository, *interval, *alertThreshold, collectAll)
	sch.SetTelegramBot(tgBot)

	// One-shot mode
	if *oneShot {
		log.Println("Running single collection cycle...")
		if collectAll {
			if err := sch.CollectOnceAllMarkets(); err != nil {
				log.Fatalf("Collection failed: %v", err)
			}
		} else {
			if err := sch.CollectOnce(); err != nil {
				log.Fatalf("Collection failed: %v", err)
			}
		}
		log.Println("Collection completed")
		return
	}

	// Continuous mode
	mode := "all DEXes"
	if !collectAll {
		mode = "native DEX only"
	}
	log.Printf("Starting OI Monitor (interval: %v, mode: %s)", *interval, mode)
	log.Println("Press Ctrl+C to stop")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	errChan := make(chan error, 1)
	go func() {
		errChan <- sch.Start(ctx)
	}()

	select {
	case sig := <-sigChan:
		log.Printf("Received signal: %v", sig)
		cancel()
		<-errChan
		log.Println("Shutdown complete")
	case err := <-errChan:
		if err != nil {
			log.Fatalf("Scheduler error: %v", err)
		}
	}
}

func showCoinHistory(repository *storage.Repository, coin string) {
	from := time.Now().Add(-24 * time.Hour)
	to := time.Now()

	records, err := repository.GetOIHistoryForCoin(coin, from, to)
	if err != nil {
		log.Fatalf("Failed to get history: %v", err)
	}

	if len(records) == 0 {
		fmt.Printf("No history found for %s\n", coin)
		return
	}

	fmt.Printf("\nOI History for %s (last 24h):\n", coin)
	fmt.Println(strings.Repeat("-", 130))
	fmt.Printf("%-19s | %6s | %6s | %12s | %15s | %12s | %10s | %10s\n",
		"Time", "DEX", "Type", "OI", "OI USD", "Mark Price", "Funding", "APR%")
	fmt.Println(strings.Repeat("-", 130))

	for _, r := range records {
		fmt.Printf("%-19s | %6s | %6s | %12.4f | %15.2f | %12.2f | %12s | %9.2f%%\n",
			r.Timestamp.Format("2006-01-02 15:04:05"),
			r.DEX, r.MarketType, r.OpenInterest, r.OpenInterestUSD, r.MarkPrice, r.Funding, r.FundingAPR)
	}
	fmt.Printf("\nTotal: %d records\n", len(records))
}

func showDEXHistory(repository *storage.Repository, dex string) {
	from := time.Now().Add(-24 * time.Hour)
	to := time.Now()

	records, err := repository.GetOIHistoryForDEX(dex, from, to)
	if err != nil {
		log.Fatalf("Failed to get history: %v", err)
	}

	if len(records) == 0 {
		fmt.Printf("No history for DEX %s\n", dex)
		return
	}

	fmt.Printf("\nOI History for DEX %s (last 24h):\n", dex)
	fmt.Println(strings.Repeat("-", 115))
	fmt.Printf("%-19s | %15s | %6s | %12s | %15s | %12s | %10s\n",
		"Time", "Coin", "Type", "OI", "OI USD", "Funding", "APR%")
	fmt.Println(strings.Repeat("-", 115))

	for _, r := range records {
		fmt.Printf("%-19s | %15s | %6s | %12.4f | %15.2f | %12s | %9.2f%%\n",
			r.Timestamp.Format("2006-01-02 15:04:05"),
			r.Coin, r.MarketType, r.OpenInterest, r.OpenInterestUSD, r.Funding, r.FundingAPR)
	}
	fmt.Printf("\nTotal: %d records\n", len(records))
}

func showRecentAlerts(repository *storage.Repository) {
	alerts, err := repository.GetRecentAlerts(50)
	if err != nil {
		log.Fatalf("Failed to get alerts: %v", err)
	}

	if len(alerts) == 0 {
		fmt.Println("No OI alerts found")
		return
	}

	fmt.Println("\nRecent OI Alerts:")
	fmt.Println(strings.Repeat("-", 90))
	for _, a := range alerts {
		fmt.Printf("%-19s | %6s | %6s | %8s | %7.1f%% | %s\n",
			a.Timestamp.Format("2006-01-02 15:04"),
			a.DEX, a.Coin, a.MarketType, a.ChangePercent, a.Direction)
	}
	fmt.Printf("\nTotal: %d alerts\n", len(alerts))
}

func showRecentFundingAlerts(repository *storage.Repository) {
	alerts, err := repository.GetRecentAlerts(50) // This needs a new method, placeholder for now
	if err != nil {
		log.Fatalf("Failed to get alerts: %v", err)
	}
	_ = alerts
	fmt.Println("Feature: showRecentFundingAlerts - add method to repository")
}

func listAllDEXs(repository *storage.Repository) {
	dexes, err := repository.GetAllDEXs()
	if err != nil {
		log.Fatalf("Failed to get DEXs: %v", err)
	}

	if len(dexes) == 0 {
		fmt.Println("No DEXs found. Run collection first.")
		return
	}

	fmt.Println("\nDEXs in database:")
	for _, dex := range dexes {
		fmt.Printf("  - %s\n", dex)
	}
	fmt.Printf("\nTotal: %d DEXs\n", len(dexes))
}
