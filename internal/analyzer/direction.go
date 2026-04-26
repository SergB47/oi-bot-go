package analyzer

import "math"

// DirectionDetector determines trade direction
type DirectionDetector struct {
	fundingFreshThresholdMinutes float64
}

// NewDirectionDetector creates detector with 10min freshness threshold
func NewDirectionDetector() *DirectionDetector {
	return &DirectionDetector{
		fundingFreshThresholdMinutes: 10.0,
	}
}

// Detect determines direction using best available method
func (dd *DirectionDetector) Detect(input DirectionInput) *DirectionResult {
	// Try funding-based first if fresh
	if input.FundingFresh {
		fundingDir := dd.detectByFunding(input.FundingCurrent, input.FundingPrevious)
		if fundingDir == DirectionLong || fundingDir == DirectionShort {
			// Confidence depends on magnitude of change, not just freshness
			confidence := dd.calculateFundingConfidence(input)
			return &DirectionResult{
				Direction:  fundingDir,
				Confidence: confidence,
				Method:     "funding",
			}
		}
		// Funding fresh but unclear direction - fall through to price
	}

	// Fallback to price-based
	priceDir := dd.detectByPrice(input)

	// Calculate confidence based on agreement with historical bias
	confidence := dd.calculateConfidence(input, priceDir)

	if priceDir == DirectionLong || priceDir == DirectionShort {
		return &DirectionResult{
			Direction:  priceDir,
			Confidence: confidence,
			Method:     "price",
		}
	}

	// Check historical bias as last resort
	if input.HistoricalFundingMean > 0.0001 {
		return &DirectionResult{
			Direction:  DirectionLong,
			Confidence: ConfidenceLow,
			Method:     "historical_bias",
		}
	} else if input.HistoricalFundingMean < -0.0001 {
		return &DirectionResult{
			Direction:  DirectionShort,
			Confidence: ConfidenceLow,
			Method:     "historical_bias",
		}
	}

	return &DirectionResult{
		Direction:  DirectionUncertain,
		Confidence: ConfidenceLow,
		Method:     "none",
	}
}

// calculateFundingConfidence determines confidence based on funding change magnitude
func (dd *DirectionDetector) calculateFundingConfidence(input DirectionInput) SignalConfidence {
	// Large funding change = high confidence
	fundingChange := math.Abs(input.FundingCurrent - input.FundingPrevious)
	if fundingChange > 0.0005 { // > 0.05% change
		return ConfidenceHigh
	}
	if fundingChange > 0.0001 { // > 0.01% change
		return ConfidenceMedium
	}
	return ConfidenceLow
}

// detectByFunding determines direction based on funding rate
// Funding positive AND rising = more longs paying = LONG impulse
// Funding negative AND falling = more shorts paying = SHORT impulse
func (dd *DirectionDetector) detectByFunding(current, previous float64) Direction {
	// Funding rising and positive = more longs paying = LONG impulse
	if current > 0 && current > previous {
		return DirectionLong
	}

	// Funding falling and negative = more shorts paying = SHORT impulse
	if current < 0 && current < previous {
		return DirectionShort
	}

	// Funding positive but stable/falling = uncertain
	// Funding negative but stable/rising = uncertain
	return DirectionUnknown
}

// detectByPrice determines direction using price and OI correlation
func (dd *DirectionDetector) detectByPrice(input DirectionInput) Direction {
	markOracleDelta := 0.0
	if input.OraclePrice > 0 {
		markOracleDelta = (input.MarkPrice - input.OraclePrice) / input.OraclePrice * 100
	}

	// Strong correlation: OI rising + Price rising = LONG accumulation
	if input.OIChange30m > 5.0 && input.PriceChange30m > 1.0 {
		return DirectionLong
	}

	// Strong correlation: OI rising + Price falling = SHORT accumulation
	if input.OIChange30m > 5.0 && input.PriceChange30m < -1.0 {
		return DirectionShort
	}

	// Mark price premium/discount as additional signal
	// Positive premium = mark > oracle = longs pushing price up
	if markOracleDelta > 0.5 && input.OIChange30m > 5.0 {
		return DirectionLong
	}

	// Negative premium = mark < oracle = shorts pushing price down
	if markOracleDelta < -0.5 && input.OIChange30m > 5.0 {
		return DirectionShort
	}

	return DirectionUncertain
}

// calculateConfidence returns confidence level based on signal quality
// HIGH: fresh funding with clear direction
// MEDIUM: price direction agrees with historical funding bias
// LOW: conflicting signals or uncertain direction
func (dd *DirectionDetector) calculateConfidence(input DirectionInput, priceDir Direction) SignalConfidence {
	if input.FundingFresh {
		// Fresh funding but unclear - check if price gives us medium confidence
		if priceDir != DirectionUncertain {
			return ConfidenceMedium
		}
		return ConfidenceLow
	}

	// Stale funding - rely on price direction
	if priceDir != DirectionUncertain {
		// Check if price agrees with historical bias
		if (priceDir == DirectionLong && input.HistoricalFundingMean > 0) ||
			(priceDir == DirectionShort && input.HistoricalFundingMean < 0) {
			return ConfidenceMedium
		}
		// Price direction conflicts with historical bias
		return ConfidenceLow
	}

	return ConfidenceLow
}

// SetFundingFreshThreshold allows customization of freshness threshold (for testing)
func (dd *DirectionDetector) SetFundingFreshThreshold(minutes float64) {
	dd.fundingFreshThresholdMinutes = minutes
}

// IsFundingFresh checks if funding is fresh based on threshold
func (dd *DirectionDetector) IsFundingFresh(ageMinutes float64) bool {
	return ageMinutes <= dd.fundingFreshThresholdMinutes
}

// CalculateFundingAPR converts hourly funding rate to annual percentage rate
func CalculateFundingAPR(hourlyRate float64) float64 {
	// Funding paid every hour: 24 * 365 = 8760 times per year
	return hourlyRate * 24 * 365 * 100
}

// CalculateZScore calculates standard score
func CalculateZScore(value, mean, stdDev float64) float64 {
	if stdDev == 0 {
		return 0
	}
	zScore := (value - mean) / stdDev
	// Clamp to reasonable range
	return math.Max(-5, math.Min(5, zScore))
}
