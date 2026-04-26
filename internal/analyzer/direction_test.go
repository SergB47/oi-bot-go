package analyzer

import (
	"testing"
)

func TestDirectionDetector_Detect(t *testing.T) {
	dd := NewDirectionDetector()

	tests := []struct {
		name       string
		input      DirectionInput
		wantDir    Direction
		wantConf   SignalConfidence
		wantMethod string
	}{
		{
			name: "Fresh funding positive rising - LONG, HIGH confidence",
			input: DirectionInput{
				FundingFresh:    true,
				FundingCurrent:  0.0003,  // 0.03% hourly = ~26% APR
				FundingPrevious: 0.0001, // Rising
			},
			wantDir:    DirectionLong,
			wantConf:   ConfidenceHigh,
			wantMethod: "funding",
		},
		{
			name: "Fresh funding negative falling - SHORT, HIGH confidence",
			input: DirectionInput{
				FundingFresh:    true,
				FundingCurrent:  -0.0003,  // -0.03% hourly
				FundingPrevious: -0.0001, // Falling (more negative)
			},
			wantDir:    DirectionShort,
			wantConf:   ConfidenceHigh,
			wantMethod: "funding",
		},
		{
			name: "Stale funding + price up + OI up - LONG, MEDIUM confidence",
			input: DirectionInput{
				FundingFresh:          false,
				OIChange30m:           15.0,
				PriceChange30m:        2.0,
				HistoricalFundingMean: 0.0001, // Bullish bias
				MarkPrice:             51000,
				OraclePrice:           50000,
			},
			wantDir:    DirectionLong,
			wantConf:   ConfidenceMedium,
			wantMethod: "price",
		},
		{
			name: "Stale funding + price down + OI up - SHORT, MEDIUM confidence",
			input: DirectionInput{
				FundingFresh:          false,
				OIChange30m:           15.0,
				PriceChange30m:        -2.0,
				HistoricalFundingMean: -0.0001, // Bearish bias
				MarkPrice:             49000,
				OraclePrice:           50000,
			},
			wantDir:    DirectionShort,
			wantConf:   ConfidenceMedium,
			wantMethod: "price",
		},
		{
			name: "Conflicting signals - LOW confidence",
			input: DirectionInput{
				FundingFresh:          false,
				OIChange30m:           15.0,
				PriceChange30m:        2.0,      // Price suggests LONG
				HistoricalFundingMean: -0.0001, // But history suggests SHORT bias
				MarkPrice:             51000,
				OraclePrice:           50000,
			},
			wantDir:    DirectionLong,
			wantConf:   ConfidenceLow,
			wantMethod: "price",
		},
		{
			name: "Mark/Oracle premium > 0.5% + OI up - LONG",
			input: DirectionInput{
				FundingFresh:   false,
				OIChange30m:    10.0,
				PriceChange30m: 0.5, // Not strong enough on its own
				MarkPrice:      50500,
				OraclePrice:    50000, // 1% premium
			},
			wantDir:    DirectionLong,
			wantConf:   ConfidenceLow, // No historical bias
			wantMethod: "price",
		},
		{
			name: "Mark/Oracle discount > 0.5% + OI up - SHORT",
			input: DirectionInput{
				FundingFresh:   false,
				OIChange30m:    10.0,
				PriceChange30m: -0.5,
				MarkPrice:      49500,
				OraclePrice:    50000, // 1% discount
			},
			wantDir:    DirectionShort,
			wantConf:   ConfidenceLow, // No historical bias
			wantMethod: "price",
		},
		{
			name: "Historical bias LONG fallback",
			input: DirectionInput{
				FundingFresh:          false,
				OIChange30m:           2.0, // Not enough for price detection
				PriceChange30m:        0.5,
				HistoricalFundingMean: 0.0002, // Strong bullish bias
			},
			wantDir:    DirectionLong,
			wantConf:   ConfidenceLow,
			wantMethod: "historical_bias",
		},
		{
			name: "Historical bias SHORT fallback",
			input: DirectionInput{
				FundingFresh:          false,
				OIChange30m:           2.0,
				PriceChange30m:        -0.5,
				HistoricalFundingMean: -0.0002, // Strong bearish bias
			},
			wantDir:    DirectionShort,
			wantConf:   ConfidenceLow,
			wantMethod: "historical_bias",
		},
		{
			name: "Completely uncertain",
			input: DirectionInput{
				FundingFresh:          false,
				OIChange30m:           2.0,
				PriceChange30m:        0.0,
				HistoricalFundingMean: 0.0, // No bias
			},
			wantDir:    DirectionUncertain,
			wantConf:   ConfidenceLow,
			wantMethod: "none",
		},
		{
			name: "Fresh funding positive but stable - fall through to price",
			input: DirectionInput{
				FundingFresh:          true,
				FundingCurrent:        0.0002,
				FundingPrevious:       0.0002, // Stable, not rising
				OIChange30m:           15.0,
				PriceChange30m:        2.0,
				HistoricalFundingMean: 0.0001,
			},
			wantDir:    DirectionLong,
			wantConf:   ConfidenceMedium,
			wantMethod: "price",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dd.Detect(tt.input)

			if got.Direction != tt.wantDir {
				t.Errorf("Detect() Direction = %v, want %v", got.Direction, tt.wantDir)
			}
			if got.Confidence != tt.wantConf {
				t.Errorf("Detect() Confidence = %v, want %v", got.Confidence, tt.wantConf)
			}
			if got.Method != tt.wantMethod {
				t.Errorf("Detect() Method = %v, want %v", got.Method, tt.wantMethod)
			}
		})
	}
}

func TestDirectionDetector_detectByFunding(t *testing.T) {
	dd := NewDirectionDetector()

	tests := []struct {
		name     string
		current  float64
		previous float64
		want     Direction
	}{
		{
			name:     "Positive and rising",
			current:  0.0003,
			previous: 0.0001,
			want:     DirectionLong,
		},
		{
			name:     "Positive but falling",
			current:  0.0001,
			previous: 0.0003,
			want:     DirectionUnknown,
		},
		{
			name:     "Negative and falling",
			current:  -0.0003,
			previous: -0.0001,
			want:     DirectionShort,
		},
		{
			name:     "Negative but rising",
			current:  -0.0001,
			previous: -0.0003,
			want:     DirectionUnknown,
		},
		{
			name:     "Zero funding",
			current:  0.0,
			previous: 0.0,
			want:     DirectionUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dd.detectByFunding(tt.current, tt.previous)
			if got != tt.want {
				t.Errorf("detectByFunding() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDirectionDetector_detectByPrice(t *testing.T) {
	dd := NewDirectionDetector()

	tests := []struct {
		name  string
		input DirectionInput
		want  Direction
	}{
		{
			name: "OI up + Price up = LONG",
			input: DirectionInput{
				OIChange30m:    10.0,
				PriceChange30m: 2.0,
			},
			want: DirectionLong,
		},
		{
			name: "OI up + Price down = SHORT",
			input: DirectionInput{
				OIChange30m:    10.0,
				PriceChange30m: -2.0,
			},
			want: DirectionShort,
		},
		{
			name: "OI not rising enough",
			input: DirectionInput{
				OIChange30m:    3.0, // Below 5% threshold
				PriceChange30m: 2.0,
			},
			want: DirectionUncertain,
		},
		{
			name: "Price change not significant",
			input: DirectionInput{
				OIChange30m:    10.0,
				PriceChange30m: 0.5, // Below 1% threshold
			},
			want: DirectionUncertain,
		},
		{
			name: "Mark/Oracle premium significant",
			input: DirectionInput{
				OIChange30m:    10.0,
				PriceChange30m: 0.5,
				MarkPrice:      50500,
				OraclePrice:    50000, // 1% premium
			},
			want: DirectionLong,
		},
		{
			name: "Mark/Oracle discount significant",
			input: DirectionInput{
				OIChange30m:    10.0,
				PriceChange30m: -0.5,
				MarkPrice:      49500,
				OraclePrice:    50000, // 1% discount
			},
			want: DirectionShort,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dd.detectByPrice(tt.input)
			if got != tt.want {
				t.Errorf("detectByPrice() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalculateFundingAPR(t *testing.T) {
	tests := []struct {
		name        string
		hourlyRate  float64
		wantApprox  float64
		tolerance   float64
	}{
		{
			name:       "Typical funding rate 0.01%",
			hourlyRate: 0.0001,
			wantApprox: 87.6, // 0.0001 * 24 * 365 * 100
			tolerance:  0.1,
		},
		{
			name:       "Zero funding",
			hourlyRate: 0.0,
			wantApprox: 0.0,
			tolerance:  0.0,
		},
		{
			name:       "Negative funding",
			hourlyRate: -0.0000232768,
			wantApprox: -20.39,
			tolerance:  0.1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateFundingAPR(tt.hourlyRate)
			diff := got - tt.wantApprox
			if diff < 0 {
				diff = -diff
			}
			if diff > tt.tolerance {
				t.Errorf("CalculateFundingAPR() = %v, want %v (diff %v > tolerance %v)",
					got, tt.wantApprox, diff, tt.tolerance)
			}
		})
	}
}

func TestCalculateZScore(t *testing.T) {
	tests := []struct {
		name   string
		value  float64
		mean   float64
		stdDev float64
		want   float64
	}{
		{
			name:   "Standard case",
			value:  110,
			mean:   100,
			stdDev: 10,
			want:   1.0,
		},
		{
			name:   "Below mean",
			value:  90,
			mean:   100,
			stdDev: 10,
			want:   -1.0,
		},
		{
			name:   "Zero stddev",
			value:  100,
			mean:   100,
			stdDev: 0,
			want:   0, // Should handle gracefully
		},
		{
			name:   "Extreme value clamped",
			value:  200,
			mean:   100,
			stdDev: 10,
			want:   5.0, // Clamped to max
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateZScore(tt.value, tt.mean, tt.stdDev)
			if got != tt.want {
				t.Errorf("CalculateZScore() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDirectionDetector_IsFundingFresh(t *testing.T) {
	dd := NewDirectionDetector()

	tests := []struct {
		name        string
		ageMinutes  float64
		want        bool
	}{
		{
			name:       "Exactly at threshold",
			ageMinutes: 10.0,
			want:       true,
		},
		{
			name:       "Below threshold",
			ageMinutes: 5.0,
			want:       true,
		},
		{
			name:       "Above threshold",
			ageMinutes: 15.0,
			want:       false,
		},
		{
			name:       "Way above threshold",
			ageMinutes: 60.0,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dd.IsFundingFresh(tt.ageMinutes)
			if got != tt.want {
				t.Errorf("IsFundingFresh() = %v, want %v", got, tt.want)
			}
		})
	}
}
