package analyzer

import (
	"math"
	"testing"
)

func TestNewSignalScorer(t *testing.T) {
	scorer := NewSignalScorer()
	if scorer == nil {
		t.Error("NewSignalScorer() returned nil")
	}
}

func TestSignalScorer_CalculateScore(t *testing.T) {
	scorer := NewSignalScorer()

	tests := []struct {
		name     string
		analysis *MultiWindowAnalysis
		wantMin  float64
		wantMax  float64
	}{
		{
			name: "basic OI increase with fresh funding",
			analysis: &MultiWindowAnalysis{
				OIChange30m:          15.0,
				OIChange2h:           20.0,
				FundingFresh:         true,
				FundingChangePercent: 40.0,
				OIUSDCurrent:         1000000000.0, // $1B
				PriceChange30m:       2.0,
			},
			wantMin: 70.0,  // oi30m(30) + oi2h(30) + funding(20) + price(4) + size(0)
			wantMax: 100.0,
		},
		{
			name: "high OI change with stale funding",
			analysis: &MultiWindowAnalysis{
				OIChange30m:   20.0,
				OIChange2h:    15.0,
				FundingFresh:  false,
				FundingZScore: 2.0,
				OIUSDCurrent:  500000000.0, // $500M
				PriceChange30m: 1.5,
			},
			wantMin: 55.0,  // oi30m(35) + oi2h(22.5) + funding(10) + price(3) + size(1.8)
			wantMax: 80.0,
		},
		{
			name: "small OI change",
			analysis: &MultiWindowAnalysis{
				OIChange30m:          5.0,
				OIChange2h:           8.0,
				FundingFresh:         true,
				FundingChangePercent: 10.0,
				OIUSDCurrent:         100000000.0, // $100M
				PriceChange30m:       0.5,
			},
			wantMin: 15.0,
			wantMax: 40.0,
		},
		{
			name: "price disagreement (no bonus)",
			analysis: &MultiWindowAnalysis{
				OIChange30m:          10.0,
				OIChange2h:           12.0,
				FundingFresh:         true,
				FundingChangePercent: 20.0,
				OIUSDCurrent:         200000000.0, // $200M
				PriceChange30m:       -1.0,        // Opposite direction
			},
			wantMin: 30.0,  // oi30m(20) + oi2h(18) + funding(10) + price(0) + size(2.6)
			wantMax: 55.0,
		},
		{
			name: "very large OI - size cap at 10",
			analysis: &MultiWindowAnalysis{
				OIChange30m:          5.0,
				OIChange2h:           5.0,
				FundingFresh:         false,
				FundingZScore:        1.0,
				OIUSDCurrent:         10000000000.0, // $10B - should hit size cap
				PriceChange30m:       1.0,
			},
			wantMin: 25.0,  // oi30m(10) + oi2h(7.5) + funding(5) + price(2) + size(10)
			wantMax: 40.0,
		},
		{
			name: "extreme OI changes - capped at max",
			analysis: &MultiWindowAnalysis{
				OIChange30m:          50.0, // Would be 100 points, capped at 35
				OIChange2h:           30.0, // Would be 45 points, capped at 25
				FundingFresh:         true,
				FundingChangePercent: 100.0, // Would be 50 points, capped at 20
				OIUSDCurrent:         1000000000.0,
				PriceChange30m:       10.0, // Would be 20 points, capped at 10
			},
			wantMin: 90.0,  // 35 + 25 + 20 + 10 + size
			wantMax: 100.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scorer.CalculateScore(tt.analysis)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("CalculateScore() = %v, want between %v and %v", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestSignalScorer_CalculateScore_Components(t *testing.T) {
	scorer := NewSignalScorer()

	// Test oi30m component
	t.Run("oi30m component", func(t *testing.T) {
		analysis := &MultiWindowAnalysis{
			OIChange30m:  10.0, // Should give 20 points (10 * 2)
			FundingFresh: false,
			OIUSDCurrent: 1000000.0,
		}
		score := scorer.CalculateScore(analysis)
		// Only oi30m component should contribute (no price agreement with 0 change)
		expectedOI30m := 20.0
		if math.Abs(score-expectedOI30m) > 2.0 {
			t.Errorf("Expected score around %v, got %v", expectedOI30m, score)
		}
	})

	// Test oi2h component
	t.Run("oi2h component", func(t *testing.T) {
		analysis := &MultiWindowAnalysis{
			OIChange2h:   10.0, // Should give 15 points (10 * 1.5)
			FundingFresh: false,
			OIUSDCurrent: 1000000.0,
		}
		score := scorer.CalculateScore(analysis)
		expectedOI2h := 15.0
		if math.Abs(score-expectedOI2h) > 2.0 {
			t.Errorf("Expected score around %v, got %v", expectedOI2h, score)
		}
	})

	// Test fresh funding component
	t.Run("fresh funding component", func(t *testing.T) {
		analysis := &MultiWindowAnalysis{
			FundingFresh:         true,
			FundingChangePercent: 20.0, // Should give 10 points (20 * 0.5)
			OIUSDCurrent:         1000000.0,
		}
		score := scorer.CalculateScore(analysis)
		expectedFunding := 10.0
		if math.Abs(score-expectedFunding) > 1.0 {
			t.Errorf("Expected score around %v, got %v", expectedFunding, score)
		}
	})

	// Test stale funding component
	t.Run("stale funding component", func(t *testing.T) {
		analysis := &MultiWindowAnalysis{
			FundingFresh:  false,
			FundingZScore: 2.0, // Should give 10 points (2 * 5)
			OIUSDCurrent:  1000000.0,
		}
		score := scorer.CalculateScore(analysis)
		expectedFunding := 10.0
		if math.Abs(score-expectedFunding) > 1.0 {
			t.Errorf("Expected score around %v, got %v", expectedFunding, score)
		}
	})

	// Test price agreement bonus
	t.Run("price agreement positive", func(t *testing.T) {
		analysis := &MultiWindowAnalysis{
			OIChange30m:    10.0,
			PriceChange30m: 3.0, // Should give 6 points (3 * 2), capped at 10
			FundingFresh:   false,
			OIUSDCurrent:   1000000.0,
		}
		score := scorer.CalculateScore(analysis)
		expectedPrice := 6.0
		if score < expectedPrice {
			t.Errorf("Expected score at least %v (price bonus), got %v", expectedPrice, score)
		}
	})

	// Test no price agreement when directions differ
	t.Run("no price agreement when directions differ", func(t *testing.T) {
		analysis := &MultiWindowAnalysis{
			OIChange30m:    10.0,  // Positive
			PriceChange30m: -3.0,  // Negative - opposite direction
			FundingFresh:   false,
			OIUSDCurrent:   1000000.0,
		}
		scoreWithDisagreement := scorer.CalculateScore(analysis)

		// Same but with agreement
		analysis.PriceChange30m = 3.0
		scoreWithAgreement := scorer.CalculateScore(analysis)

		// Score with agreement should be higher by approximately the price bonus
		diff := scoreWithAgreement - scoreWithDisagreement
		if diff < 5.0 {
			t.Errorf("Expected price agreement to add at least 5 points, but difference was %v", diff)
		}
	})

	// Test size bonus
	t.Run("size bonus", func(t *testing.T) {
		analysis := &MultiWindowAnalysis{
			OIUSDCurrent: 100000000.0, // $100M
			FundingFresh: false,
		}
		score := scorer.CalculateScore(analysis)
		// log10(100M/1M) * 2 = log10(100) * 2 = 2 * 2 = 4
		expectedSize := 4.0
		if math.Abs(score-expectedSize) > 1.0 {
			t.Errorf("Expected size bonus around %v, got %v", expectedSize, score)
		}
	})
}

func TestSignalScorer_SizeBonusCalculation(t *testing.T) {
	scorer := NewSignalScorer()

	tests := []struct {
		name         string
		oiUSDCurrent float64
		wantBonus    float64
	}{
		{
			name:         "$1M OI",
			oiUSDCurrent: 1000000.0,
			wantBonus:    0.0, // log10(1) * 2 = 0
		},
		{
			name:         "$10M OI",
			oiUSDCurrent: 10000000.0,
			wantBonus:    2.0, // log10(10) * 2 = 2
		},
		{
			name:         "$100M OI",
			oiUSDCurrent: 100000000.0,
			wantBonus:    4.0, // log10(100) * 2 = 4
		},
		{
			name:         "$1B OI",
			oiUSDCurrent: 1000000000.0,
			wantBonus:    6.0, // log10(1000) * 2 = 6
		},
		{
			name:         "$10B OI (capped at 10)",
			oiUSDCurrent: 10000000000.0,
			wantBonus:    8.0, // Would be 8, not yet capped
		},
		{
			name:         "$100B OI (capped at 10)",
			oiUSDCurrent: 100000000000.0,
			wantBonus:    10.0, // log10(100000) * 2 = 10, exactly at cap
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analysis := &MultiWindowAnalysis{
				OIUSDCurrent: tt.oiUSDCurrent,
				FundingFresh: false,
			}
			score := scorer.CalculateScore(analysis)

			// The score should be approximately equal to the expected size bonus
			// (with small tolerance for floating point)
			if math.Abs(score-tt.wantBonus) > 1.0 {
				t.Errorf("Size bonus: got %v, want around %v", score, tt.wantBonus)
			}
		})
	}
}

func TestSignalScorer_CalculateScore_ZeroValues(t *testing.T) {
	scorer := NewSignalScorer()

	// Test with zero OI (edge case)
	t.Run("zero OI current", func(t *testing.T) {
		analysis := &MultiWindowAnalysis{
			OIUSDCurrent: 0.0,
			FundingFresh: false,
		}
		score := scorer.CalculateScore(analysis)
		if score != 0.0 {
			t.Errorf("Expected 0 score for zero OI, got %v", score)
		}
	})

	// Test with negative OI (shouldn't happen but test safety)
	t.Run("negative OI (safety)", func(t *testing.T) {
		analysis := &MultiWindowAnalysis{
			OIChange30m:  -10.0, // Negative change
			PriceChange30m: -2.0, // Same direction
			FundingFresh: false,
			OIUSDCurrent: 100000000.0,
		}
		score := scorer.CalculateScore(analysis)
		// Should still calculate using absolute values
		if score < 10.0 {
			t.Errorf("Expected positive score even with negative OI change, got %v", score)
		}
	})
}

func TestSignalScorer_CalculateScore_CappedComponents(t *testing.T) {
	scorer := NewSignalScorer()

	// Test that all components are properly capped
	t.Run("all components at max", func(t *testing.T) {
		analysis := &MultiWindowAnalysis{
			OIChange30m:          20.0,  // Would be 40, capped at 35
			OIChange2h:           20.0,  // Would be 30, capped at 25
			FundingFresh:         true,
			FundingChangePercent: 50.0,  // Would be 25, capped at 20
			PriceChange30m:       10.0,  // Would be 20, capped at 10
			OIUSDCurrent:         100000000000.0, // $100B, log10(100000) * 2 = 10
		}
		score := scorer.CalculateScore(analysis)

		// Maximum possible: 35 + 25 + 20 + 10 + 10 = 100
		expectedMax := 100.0
		if score > expectedMax {
			t.Errorf("Score %v exceeds maximum expected %v", score, expectedMax)
		}
		if score < 95.0 {
			t.Errorf("Expected score close to maximum, got %v", score)
		}
	})

	// Test individual caps
	t.Run("oi30m cap at 35", func(t *testing.T) {
		analysis := &MultiWindowAnalysis{
			OIChange30m:  30.0, // Would be 60 without cap
			FundingFresh: false,
			OIUSDCurrent: 1000000.0,
		}
		score := scorer.CalculateScore(analysis)
		// Should be capped at 35 (plus tiny size bonus)
		if score > 40.0 {
			t.Errorf("oi30m component should be capped at 35, got total score %v", score)
		}
	})
}


func TestSignalScorer_PriceAgreementVsNoAgreement(t *testing.T) {
	scorer := NewSignalScorer()

	// Compare two scenarios: one with agreement, one without
	baseOIChange := 10.0
	basePriceChange := 3.0

	// Scenario 1: Agreement (both positive)
	agreeAnalysis := &MultiWindowAnalysis{
		OIChange30m:    baseOIChange,
		PriceChange30m: basePriceChange,
		FundingFresh:   false,
		OIUSDCurrent:   1000000.0,
	}
	agreeScore := scorer.CalculateScore(agreeAnalysis)

	// Scenario 2: Disagreement (OI positive, price negative)
	disagreeAnalysis := &MultiWindowAnalysis{
		OIChange30m:    baseOIChange,
		PriceChange30m: -basePriceChange,
		FundingFresh:   false,
		OIUSDCurrent:   1000000.0,
	}
	disagreeScore := scorer.CalculateScore(disagreeAnalysis)

	// The difference should be approximately the price agreement bonus
	// Price bonus = min(|3| * 2, 10) = 6
	expectedBonus := 6.0
	diff := agreeScore - disagreeScore

	if math.Abs(diff-expectedBonus) > 1.0 {
		t.Errorf("Price agreement bonus difference = %v, expected around %v", diff, expectedBonus)
	}
}

// Benchmark the scoring function
func BenchmarkSignalScorer_CalculateScore(b *testing.B) {
	scorer := NewSignalScorer()
	analysis := &MultiWindowAnalysis{
		OIChange30m:          15.0,
		OIChange2h:           20.0,
		FundingFresh:         true,
		FundingChangePercent: 30.0,
		OIUSDCurrent:         1000000000.0,
		PriceChange30m:       2.5,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scorer.CalculateScore(analysis)
	}
}
