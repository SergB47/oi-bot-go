package analyzer

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"
)

// InstrumentStats holds rolling 14-day statistics
type InstrumentStats struct {
	Coin             string
	DEX              string
	FundingMean      float64
	FundingStdDev    float64
	FundingP50       float64
	OIMean           float64
	OIStdDev         float64
	PriceMean        float64
	PriceVolatility  float64
	LastCalculatedAt time.Time
	DataPointsCount  int
}

// HistoryRecord represents a single OI history record for stats calculation
type HistoryRecord struct {
	Coin            string
	DEX             string
	OpenInterestUSD float64
	MarkPrice       float64
	Funding         string
	Timestamp       time.Time
}

// HistoryProvider is a function type that provides history records
type HistoryProvider func(coin, dex string, from, to time.Time) ([]HistoryRecord, error)

// StatsCalculator calculates rolling statistics
type StatsCalculator struct {
	historyProvider HistoryProvider
}

// NewStatsCalculator creates new calculator with a history provider function
func NewStatsCalculator(provider HistoryProvider) *StatsCalculator {
	return &StatsCalculator{historyProvider: provider}
}

// CalculateStats computes 14-day rolling stats
func (sc *StatsCalculator) CalculateStats(coin, dex string) (*InstrumentStats, error) {
	from := time.Now().AddDate(0, 0, -14)
	to := time.Now()

	records, err := sc.historyProvider(coin, dex, from, to)
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
