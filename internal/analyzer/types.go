package analyzer

// Direction represents trading direction
type Direction string

const (
	DirectionLong      Direction = "long"
	DirectionShort     Direction = "short"
	DirectionUncertain Direction = "uncertain"
	DirectionUnknown   Direction = "unknown"
)

// SignalConfidence represents confidence level
type SignalConfidence string

const (
	ConfidenceHigh   SignalConfidence = "high"
	ConfidenceMedium SignalConfidence = "medium"
	ConfidenceLow    SignalConfidence = "low"
)

// DirectionResult holds detection result
type DirectionResult struct {
	Direction  Direction
	Confidence SignalConfidence
	Method     string // "funding" or "price" or "historical_bias" or "none"
}

// DirectionInput holds all inputs for detection
type DirectionInput struct {
	OIChange30m           float64
	PriceChange30m        float64
	FundingCurrent        float64
	FundingPrevious       float64
	HistoricalFundingMean float64
	FundingFresh          bool
	MarkPrice             float64
	OraclePrice           float64
}
