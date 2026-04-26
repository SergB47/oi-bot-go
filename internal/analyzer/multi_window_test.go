package analyzer

import (
	"math"
	"testing"
	"time"
)

// mockOIProvider creates a mock OI provider for testing
func mockOIProvider(data map[string]float64) OIProvider {
	return func(coin, dex string, targetTime time.Time) (float64, error) {
		// Create a key based on coin, dex and rough time window
		var key string
		now := time.Now()
		age := now.Sub(targetTime)

		switch {
		case age >= 23*time.Hour:
			key = coin + "/" + dex + "/24h"
		case age >= 1*time.Hour+30*time.Minute:
			key = coin + "/" + dex + "/2h"
		case age >= 25*time.Minute:
			key = coin + "/" + dex + "/30m"
		default:
			key = coin + "/" + dex + "/current"
		}

		if val, ok := data[key]; ok {
			return val, nil
		}
		return 0, nil
	}
}

// mockPriceProvider creates a mock price provider for testing
func mockPriceProvider(data map[string]float64) PriceProvider {
	return func(coin, dex string, targetTime time.Time) (float64, error) {
		var key string
		now := time.Now()
		age := now.Sub(targetTime)

		switch {
		case age >= 1*time.Hour+30*time.Minute:
			key = coin + "/" + dex + "/2h"
		case age >= 25*time.Minute:
			key = coin + "/" + dex + "/30m"
		default:
			key = coin + "/" + dex + "/current"
		}

		if val, ok := data[key]; ok {
			return val, nil
		}
		return 0, nil
	}
}

func TestCalculateChangePercent(t *testing.T) {
	tests := []struct {
		name     string
		previous float64
		current  float64
		want     float64
	}{
		{
			name:     "positive change",
			previous: 100.0,
			current:  135.0,
			want:     35.0,
		},
		{
			name:     "negative change",
			previous: 100.0,
			current:  80.0,
			want:     -20.0,
		},
		{
			name:     "zero previous returns 0",
			previous: 0.0,
			current:  100.0,
			want:     0.0,
		},
		{
			name:     "no change",
			previous: 100.0,
			current:  100.0,
			want:     0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateChangePercent(tt.previous, tt.current)
			if math.Abs(got-tt.want) > 0.001 {
				t.Errorf("calculateChangePercent() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMultiWindowAnalysis_ShouldAlertInstant(t *testing.T) {
	tests := []struct {
		name      string
		analysis  MultiWindowAnalysis
		threshold float64
		want      bool
	}{
		{
			name:      "OI change 30m exceeds threshold",
			analysis:  MultiWindowAnalysis{OIChange30m: 35.0},
			threshold: 30.0,
			want:      true,
		},
		{
			name:      "OI change 2h exceeds threshold",
			analysis:  MultiWindowAnalysis{OIChange2h: -35.0},
			threshold: 30.0,
			want:      true,
		},
		{
			name:      "Funding Z-score exceeds 3",
			analysis:  MultiWindowAnalysis{FundingZScore: 3.5},
			threshold: 30.0,
			want:      true,
		},
		{
			name:      "Fresh funding with >50% change",
			analysis:  MultiWindowAnalysis{FundingFresh: true, FundingChangePercent: 60.0},
			threshold: 30.0,
			want:      true,
		},
		{
			name:      "No alert - below all thresholds",
			analysis:  MultiWindowAnalysis{OIChange30m: 20.0, OIChange2h: 15.0, FundingZScore: 2.0, FundingFresh: false},
			threshold: 30.0,
			want:      false,
		},
		{
			name:      "Fresh funding but only 40% change",
			analysis:  MultiWindowAnalysis{FundingFresh: true, FundingChangePercent: 40.0},
			threshold: 30.0,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.analysis.ShouldAlertInstant(tt.threshold)
			if got != tt.want {
				t.Errorf("ShouldAlertInstant() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMultiWindowAnalysis_ShouldAlertPeriodic(t *testing.T) {
	tests := []struct {
		name     string
		analysis MultiWindowAnalysis
		want     bool
	}{
		{
			name:     "OI 30m > 15%",
			analysis: MultiWindowAnalysis{OIChange30m: 16.0},
			want:     true,
		},
		{
			name:     "OI 2h > 20%",
			analysis: MultiWindowAnalysis{OIChange2h: 21.0},
			want:     true,
		},
		{
			name:     "OI 24h > 30%",
			analysis: MultiWindowAnalysis{OIChange24h: 31.0},
			want:     true,
		},
		{
			name:     "Funding Z-score > 2",
			analysis: MultiWindowAnalysis{FundingZScore: 2.5},
			want:     true,
		},
		{
			name:     "Fresh funding change > 30%",
			analysis: MultiWindowAnalysis{FundingFresh: true, FundingChangePercent: 35.0},
			want:     true,
		},
		{
			name:     "No alert - below thresholds",
			analysis: MultiWindowAnalysis{OIChange30m: 10.0, OIChange2h: 15.0, OIChange24h: 20.0, FundingZScore: 1.5, FundingFresh: false},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.analysis.ShouldAlertPeriodic()
			if got != tt.want {
				t.Errorf("ShouldAlertPeriodic() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMultiWindowAnalysis_DataStructure(t *testing.T) {
	// Test that MultiWindowAnalysis holds all expected fields
	analysis := MultiWindowAnalysis{
		Coin:                  "BTC",
		DEX:                   "native",
		MarketType:            "perp",
		OIChange30m:           15.5,
		OIChange2h:            25.0,
		OIChange24h:           45.0,
		OIUSDCurrent:          1000000000.0,
		OIUSDPrevious:         950000000.0,
		FundingCurrent:        0.0001,
		FundingPrevious:       0.00008,
		FundingChangePercent:  25.0,
		FundingZScore:         2.5,
		FundingAPR:            87.6,
		FundingFresh:          true,
		PriceChange30m:        2.5,
		PriceChange2h:         5.0,
		MarkPrice:             65000.0,
		OraclePrice:           64950.0,
		MarkOracleDelta:       0.077,
		HistoricalFundingMean: 0.00005,
	}

	// Verify all fields are accessible
	if analysis.Coin != "BTC" {
		t.Error("Coin field incorrect")
	}
	if analysis.DEX != "native" {
		t.Error("DEX field incorrect")
	}
	if analysis.MarketType != "perp" {
		t.Error("MarketType field incorrect")
	}
	if analysis.OIChange30m != 15.5 {
		t.Error("OIChange30m field incorrect")
	}
	if analysis.OIUSDCurrent != 1000000000.0 {
		t.Error("OIUSDCurrent field incorrect")
	}
	if analysis.FundingAPR != 87.6 {
		t.Error("FundingAPR field incorrect")
	}
	if !analysis.FundingFresh {
		t.Error("FundingFresh field incorrect")
	}
	if analysis.MarkOracleDelta != 0.077 {
		t.Error("MarkOracleDelta field incorrect")
	}
}

func TestNewMultiWindowAnalyzer(t *testing.T) {
	analyzer := NewMultiWindowAnalyzer(nil, nil)
	if analyzer == nil {
		t.Error("NewMultiWindowAnalyzer() returned nil")
	}
	if analyzer.oiProvider != nil {
		t.Error("Expected nil oiProvider")
	}
	if analyzer.priceProvider != nil {
		t.Error("Expected nil priceProvider")
	}
}

func TestNewMultiWindowAnalyzer_WithProviders(t *testing.T) {
	oiProvider := func(coin, dex string, targetTime time.Time) (float64, error) {
		return 1000000.0, nil
	}
	priceProvider := func(coin, dex string, targetTime time.Time) (float64, error) {
		return 50000.0, nil
	}

	analyzer := NewMultiWindowAnalyzer(oiProvider, priceProvider)
	if analyzer == nil {
		t.Fatal("NewMultiWindowAnalyzer() returned nil")
	}
	if analyzer.oiProvider == nil {
		t.Error("Expected non-nil oiProvider")
	}
	if analyzer.priceProvider == nil {
		t.Error("Expected non-nil priceProvider")
	}
}

func TestMultiWindowAnalyzer_Analyze(t *testing.T) {
	// Set up mock data
	oiData := map[string]float64{
		"BTC/native/30m": 950000000.0,  // $950M
		"BTC/native/2h":  900000000.0,  // $900M
		"BTC/native/24h": 800000000.0,  // $800M
	}
	priceData := map[string]float64{
		"BTC/native/30m": 63000.0,
		"BTC/native/2h":  62000.0,
	}

	oiProvider := mockOIProvider(oiData)
	priceProvider := mockPriceProvider(priceData)
	analyzer := NewMultiWindowAnalyzer(oiProvider, priceProvider)

	stats := &InstrumentStats{
		FundingMean:   0.0001,
		FundingStdDev: 0.00005,
	}

	analysis, err := analyzer.Analyze(
		"BTC", "native", "perp",
		1000000000.0, // Current OI: $1B
		0.00012,      // Current funding
		65000.0,      // Mark price
		64950.0,      // Oracle price
		stats,
	)

	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	// Verify calculations
	// OI change 30m: ((1B - 950M) / 950M) * 100 = 5.26%
	expectedOI30m := 5.263
	if math.Abs(analysis.OIChange30m-expectedOI30m) > 0.1 {
		t.Errorf("OIChange30m = %v, want approx %v", analysis.OIChange30m, expectedOI30m)
	}

	// OI change 2h: ((1B - 900M) / 900M) * 100 = 11.11%
	expectedOI2h := 11.11
	if math.Abs(analysis.OIChange2h-expectedOI2h) > 0.1 {
		t.Errorf("OIChange2h = %v, want approx %v", analysis.OIChange2h, expectedOI2h)
	}

	// OI change 24h: ((1B - 800M) / 800M) * 100 = 25%
	expectedOI24h := 25.0
	if math.Abs(analysis.OIChange24h-expectedOI24h) > 0.1 {
		t.Errorf("OIChange24h = %v, want approx %v", analysis.OIChange24h, expectedOI24h)
	}

	// Price change 30m: ((65000 - 63000) / 63000) * 100 = 3.17%
	expectedPrice30m := 3.17
	if math.Abs(analysis.PriceChange30m-expectedPrice30m) > 0.1 {
		t.Errorf("PriceChange30m = %v, want approx %v", analysis.PriceChange30m, expectedPrice30m)
	}

	// Mark/oracle delta: ((65000 - 64950) / 64950) * 100 = 0.077%
	expectedDelta := 0.077
	if math.Abs(analysis.MarkOracleDelta-expectedDelta) > 0.01 {
		t.Errorf("MarkOracleDelta = %v, want approx %v", analysis.MarkOracleDelta, expectedDelta)
	}

	// Funding Z-score: (0.00012 - 0.0001) / 0.00005 = 0.4
	expectedZScore := 0.4
	if math.Abs(analysis.FundingZScore-expectedZScore) > 0.01 {
		t.Errorf("FundingZScore = %v, want approx %v", analysis.FundingZScore, expectedZScore)
	}

	// Verify other fields
	if analysis.Coin != "BTC" {
		t.Error("Coin not set correctly")
	}
	if analysis.DEX != "native" {
		t.Error("DEX not set correctly")
	}
	if analysis.OIUSDCurrent != 1000000000.0 {
		t.Error("OIUSDCurrent not set correctly")
	}
	if analysis.HistoricalFundingMean != 0.0001 {
		t.Error("HistoricalFundingMean not set correctly")
	}
}

func TestMultiWindowAnalyzer_Analyze_NoProviders(t *testing.T) {
	analyzer := NewMultiWindowAnalyzer(nil, nil)

	analysis, err := analyzer.Analyze(
		"BTC", "native", "perp",
		1000000000.0,
		0.0001,
		65000.0,
		64950.0,
		nil,
	)

	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	// Without providers, all changes should be 0
	if analysis.OIChange30m != 0 {
		t.Errorf("Expected OIChange30m = 0 without provider, got %v", analysis.OIChange30m)
	}
	if analysis.OIChange2h != 0 {
		t.Errorf("Expected OIChange2h = 0 without provider, got %v", analysis.OIChange2h)
	}
	if analysis.PriceChange30m != 0 {
		t.Errorf("Expected PriceChange30m = 0 without provider, got %v", analysis.PriceChange30m)
	}
}

func TestMultiWindowAnalyzer_Analyze_NoHistoricalData(t *testing.T) {
	// Providers return 0 (no data)
	emptyProvider := func(coin, dex string, targetTime time.Time) (float64, error) {
		return 0, nil
	}

	analyzer := NewMultiWindowAnalyzer(emptyProvider, emptyProvider)

	analysis, err := analyzer.Analyze(
		"BTC", "native", "perp",
		1000000000.0,
		0.0001,
		65000.0,
		64950.0,
		nil,
	)

	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	// Without historical data, changes should be 0
	if analysis.OIChange30m != 0 {
		t.Errorf("Expected OIChange30m = 0 without historical data, got %v", analysis.OIChange30m)
	}
}

func TestCalculateChangePercent_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		previous float64
		current  float64
		want     float64
	}{
		{
			name:     "small values",
			previous: 0.001,
			current:  0.0015,
			want:     50.0,
		},
		{
			name:     "large values",
			previous: 1e12,
			current:  1.1e12,
			want:     10.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateChangePercent(tt.previous, tt.current)
			if math.Abs(got-tt.want) > 0.001 {
				t.Errorf("calculateChangePercent() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMultiWindowAnalysis_ShouldAlertInstant_ZScoreNegative(t *testing.T) {
	analysis := MultiWindowAnalysis{
		FundingZScore: -3.5,
	}
	if !analysis.ShouldAlertInstant(30.0) {
		t.Error("ShouldAlertInstant should trigger for negative Z-score > 3")
	}
}

func TestMultiWindowAnalysis_ShouldAlertPeriodic_FundingFreshBoundary(t *testing.T) {
	// Test boundary at exactly 30%
	analysis := MultiWindowAnalysis{
		FundingFresh:         true,
		FundingChangePercent: 30.0,
	}
	// Should not trigger at exactly 30% (needs to be > 30)
	if analysis.ShouldAlertPeriodic() {
		t.Error("ShouldAlertPeriodic should not trigger at exactly 30%")
	}

	// Should trigger at 30.1%
	analysis.FundingChangePercent = 30.1
	if !analysis.ShouldAlertPeriodic() {
		t.Error("ShouldAlertPeriodic should trigger at > 30%")
	}
}

func TestMultiWindowAnalyzer_FundingAPR(t *testing.T) {
	oiProvider := func(coin, dex string, targetTime time.Time) (float64, error) {
		return 0, nil
	}
	priceProvider := func(coin, dex string, targetTime time.Time) (float64, error) {
		return 0, nil
	}

	analyzer := NewMultiWindowAnalyzer(oiProvider, priceProvider)

	// Funding rate 0.0001 (0.01% per hour)
	// APR = 0.0001 * 24 * 365 * 100 = 87.6%
	analysis, _ := analyzer.Analyze(
		"BTC", "native", "perp",
		1000000.0,
		0.0001, // Hourly funding rate
		50000.0,
		50000.0,
		nil,
	)

	expectedAPR := 87.6
	if math.Abs(analysis.FundingAPR-expectedAPR) > 0.1 {
		t.Errorf("FundingAPR = %v, want %v", analysis.FundingAPR, expectedAPR)
	}
}
