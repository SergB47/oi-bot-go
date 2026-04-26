package analyzer

import (
	"log"
	"math"
)

// SignalScorer calculates composite score for momentum signals
type SignalScorer struct{}

// NewSignalScorer creates scorer
func NewSignalScorer() *SignalScorer {
	return &SignalScorer{}
}

// CalculateScore computes momentum-focused score
func (ss *SignalScorer) CalculateScore(analysis *MultiWindowAnalysis) float64 {
	if analysis == nil {
		return 0
	}

	// Validate inputs to prevent NaN/Inf propagation
	if math.IsNaN(analysis.OIChange30m) || math.IsInf(analysis.OIChange30m, 0) ||
		math.IsNaN(analysis.OIChange2h) || math.IsInf(analysis.OIChange2h, 0) ||
		math.IsNaN(analysis.FundingZScore) || math.IsInf(analysis.FundingZScore, 0) {
		log.Printf("Invalid analysis values detected (NaN/Inf)")
		return 0
	}

	// Speed component (immediate impulse) - max 35 points
	oi30mComponent := math.Min(math.Abs(analysis.OIChange30m)*2, 35)

	// Trend component (sustained move) - max 25 points
	oi2hComponent := math.Min(math.Abs(analysis.OIChange2h)*1.5, 25)

	// Funding confirmation - max 20 if fresh, max 10 if stale
	fundingComponent := 0.0
	if analysis.FundingFresh {
		fundingComponent = math.Min(math.Abs(analysis.FundingChangePercent)*0.5, 20)
	} else {
		fundingComponent = math.Min(math.Abs(analysis.FundingZScore)*5, 10)
	}

	// Price agreement bonus (when price and OI move together) - max 10 points
	priceAgreement := 0.0
	if (analysis.OIChange30m > 0 && analysis.PriceChange30m > 0) ||
		(analysis.OIChange30m < 0 && analysis.PriceChange30m < 0) {
		priceAgreement = math.Min(math.Abs(analysis.PriceChange30m)*2, 10)
	}

	// Size component - instruments with larger OI get bonus - max 10 points
	sizeBonus := 0.0
	if analysis.OIUSDCurrent > 0 {
		sizeBonus = math.Min(math.Log10(analysis.OIUSDCurrent/1e6)*2, 10)
		if sizeBonus < 0 {
			sizeBonus = 0
		}
	}

	return oi30mComponent + oi2hComponent + fundingComponent + priceAgreement + sizeBonus
}
