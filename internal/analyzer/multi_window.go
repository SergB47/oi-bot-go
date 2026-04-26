package analyzer

import (
	"fmt"
	"log"
	"math"
	"time"
)

// MultiWindowAnalysis holds changes across time windows
type MultiWindowAnalysis struct {
	Coin       string
	DEX        string
	MarketType string

	// OI changes (%)
	OIChange30m   float64
	OIChange2h    float64
	OIChange24h   float64
	OIUSDCurrent  float64
	OIUSDPrevious float64

	// Funding
	FundingCurrent       float64
	FundingPrevious      float64
	FundingChangePercent float64
	FundingZScore        float64
	FundingAPR           float64
	FundingFresh         bool

	// Price context
	PriceChange30m float64
	PriceChange2h  float64
	MarkPrice      float64
	OraclePrice    float64
	MarkOracleDelta float64

	// Historical context
	HistoricalFundingMean float64
}

// OIProvider is a function type that provides OI value at a specific time
type OIProvider func(coin, dex string, targetTime time.Time) (float64, error)

// PriceProvider is a function type that provides mark price at a specific time
type PriceProvider func(coin, dex string, targetTime time.Time) (float64, error)

// MultiWindowAnalyzer performs multi-timeframe analysis
type MultiWindowAnalyzer struct {
	oiProvider    OIProvider
	priceProvider PriceProvider
}

// NewMultiWindowAnalyzer creates analyzer with providers
func NewMultiWindowAnalyzer(oiProvider OIProvider, priceProvider PriceProvider) *MultiWindowAnalyzer {
	return &MultiWindowAnalyzer{
		oiProvider:    oiProvider,
		priceProvider: priceProvider,
	}
}

// Analyze performs multi-window analysis
func (mwa *MultiWindowAnalyzer) Analyze(
	coin, dex, marketType string,
	currentOI, currentFunding, currentMarkPrice, currentOraclePrice float64,
	stats *InstrumentStats,
) (*MultiWindowAnalysis, error) {

	now := time.Now()

	// Get historical OI snapshots
	oi30mAgo, err := mwa.getOIAtTime(coin, dex, now.Add(-30*time.Minute))
	if err != nil {
		log.Printf("Failed to get 30m OI for %s/%s: %v", coin, dex, err)
	}
	oi2hAgo, err := mwa.getOIAtTime(coin, dex, now.Add(-2*time.Hour))
	if err != nil {
		log.Printf("Failed to get 2h OI for %s/%s: %v", coin, dex, err)
	}
	oi24hAgo, err := mwa.getOIAtTime(coin, dex, now.Add(-24*time.Hour))
	if err != nil {
		log.Printf("Failed to get 24h OI for %s/%s: %v", coin, dex, err)
	}

	// Get historical price snapshots
	price30mAgo, err := mwa.getPriceAtTime(coin, dex, now.Add(-30*time.Minute))
	if err != nil {
		log.Printf("Failed to get 30m price for %s/%s: %v", coin, dex, err)
	}
	price2hAgo, err := mwa.getPriceAtTime(coin, dex, now.Add(-2*time.Hour))
	if err != nil {
		log.Printf("Failed to get 2h price for %s/%s: %v", coin, dex, err)
	}

	analysis := &MultiWindowAnalysis{
		Coin:           coin,
		DEX:            dex,
		MarketType:     marketType,
		OIUSDCurrent:   currentOI,
		FundingCurrent: currentFunding,
		MarkPrice:      currentMarkPrice,
		OraclePrice:    currentOraclePrice,
		FundingAPR:     CalculateFundingAPR(currentFunding),
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
		analysis.FundingZScore = CalculateZScore(currentFunding, stats.FundingMean, stats.FundingStdDev)
		analysis.HistoricalFundingMean = stats.FundingMean
	}

	return analysis, nil
}

// ShouldAlertInstant checks if triggers instant alert (>30% or extreme Z-score)
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

// getOIAtTime safely retrieves OI at a specific time, returning -1 as sentinel for errors
func (mwa *MultiWindowAnalyzer) getOIAtTime(coin, dex string, targetTime time.Time) (float64, error) {
	if mwa.oiProvider == nil {
		return 0, nil
	}
	oi, err := mwa.oiProvider(coin, dex, targetTime)
	if err != nil {
		return -1, err // Use -1 as sentinel for "unknown"
	}
	if oi < 0 {
		return -1, fmt.Errorf("negative OI value: %f", oi)
	}
	return oi, nil
}

// getPriceAtTime safely retrieves price at a specific time, returning -1 as sentinel for errors
func (mwa *MultiWindowAnalyzer) getPriceAtTime(coin, dex string, targetTime time.Time) (float64, error) {
	if mwa.priceProvider == nil {
		return 0, nil
	}
	price, err := mwa.priceProvider(coin, dex, targetTime)
	if err != nil {
		return -1, err // Use -1 as sentinel for "unknown"
	}
	if price < 0 {
		return -1, fmt.Errorf("negative price value: %f", price)
	}
	return price, nil
}

// calculateChangePercent calculates percentage change between two values
func calculateChangePercent(previous, current float64) float64 {
	if previous == 0 {
		return 0
	}
	return ((current - previous) / previous) * 100
}
