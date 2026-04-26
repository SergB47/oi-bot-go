package telegram

import (
	"fmt"
	"math"
	"oi_bot_go/internal/storage"
	"strings"
	"time"
)

// Telegram message limits
const (
	TelegramLimit = 4096
	SafetyMargin  = 500
	MaxLength     = TelegramLimit - SafetyMargin
)

// DigestBuilder builds momentum-focused digests
type DigestBuilder struct{}

// NewDigestBuilder creates a new digest builder
func NewDigestBuilder() *DigestBuilder {
	return &DigestBuilder{}
}

// BuildDigest creates digest messages grouped by direction (LONG/SHORT/UNCERTAIN)
func (db *DigestBuilder) BuildDigest(longSignals, shortSignals, uncertainSignals []storage.SignalQueueRecord, totalInstruments int) []string {
	if len(longSignals) == 0 && len(shortSignals) == 0 && len(uncertainSignals) == 0 {
		return nil
	}

	totalSignals := len(longSignals) + len(shortSignals) + len(uncertainSignals)

	var messages []string
	var current strings.Builder

	// Header
	header := fmt.Sprintf("🚀 Momentum Signals | %s | %d signals\n\n",
		time.Now().Format("15:04 UTC"), totalSignals)
	current.WriteString(header)

	// Market context
	context := fmt.Sprintf("📊 Market: %d instruments | 🟩 %d | 🟥 %d | ⚪ %d\n\n",
		totalInstruments, len(longSignals), len(shortSignals), len(uncertainSignals))
	current.WriteString(context)

	// LONG section
	if len(longSignals) > 0 {
		section := db.formatDirectionSection("🟩 LONG IMPULSE", longSignals)
		if current.Len()+len(section) > MaxLength {
			messages = append(messages, current.String())
			current.Reset()
			current.WriteString("🚀 Signals (continued)...\n\n")
		}
		current.WriteString(section)
	}

	// SHORT section
	if len(shortSignals) > 0 {
		section := db.formatDirectionSection("🟥 SHORT IMPULSE", shortSignals)
		if current.Len()+len(section) > MaxLength {
			messages = append(messages, current.String())
			current.Reset()
			current.WriteString("🚀 Signals (continued)...\n\n")
		}
		current.WriteString(section)
	}

	// UNCERTAIN section
	if len(uncertainSignals) > 0 {
		section := db.formatDirectionSection("⚪ UNCERTAIN", uncertainSignals)
		if current.Len()+len(section) > MaxLength {
			messages = append(messages, current.String())
			current.Reset()
			current.WriteString("🚀 Signals (continued)...\n\n")
		}
		current.WriteString(section)
	}

	// Top movers
	topMovers := db.formatTopMovers(append(append(longSignals, shortSignals...), uncertainSignals...), 5)
	if current.Len()+len(topMovers) > MaxLength {
		messages = append(messages, current.String())
		current.Reset()
		current.WriteString("🚀 Signals (continued)...\n\n")
	}
	current.WriteString(topMovers)

	// Footer
	current.WriteString(fmt.Sprintf("\n🕐 Next digest: %s\n",
		time.Now().Add(30*time.Minute).Format("15:04 UTC")))
	messages = append(messages, current.String())

	return messages
}

// BuildInstantAlert builds an instant alert for >30% moves
func (db *DigestBuilder) BuildInstantAlert(s storage.SignalQueueRecord) string {
	direction := "🟩 LONG"
	if s.SignalDirection == "short" {
		direction = "🟥 SHORT"
	}

	freshness := ""
	if s.FundingFresh {
		freshness = " 🆕"
	}

	return fmt.Sprintf(
		"🚨 CRITICAL MOMENTUM | %s\n\n"+
		"🔥 %s/%s: %s impulse accelerating\n"+
		"━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"+
		"OI: %s in 30min | %s\n"+
		"Funding: %.0f%% APR%s | Z-score: %.1f\n"+
		"Price: %s (mark vs 30m ago)\n\n"+
		"Classification: %s | %s CONFIDENCE\n\n"+
		"Context:\n"+
		"• 30m: %s | 2h: %s | 24h: %s\n"+
		"• Mark/Oracle: %.2f%%\n"+
		"• Signal strength: %.0f/100\n",
		time.Now().Format("15:04 UTC"),
		s.Coin, s.DEX, direction,
		formatChange(s.OIChange30m), formatUSD(s.OIUSDCurrent),
		s.FundingAPRCurrent, freshness, s.FundingZScore,
		formatChange(s.PriceChange30m),
		direction, strings.ToUpper(s.SignalConfidence),
		formatChange(s.OIChange30m), formatChange(s.OIChange2h), formatChange(s.OIChange24h),
		s.MarkOracleDelta,
		s.CompositeScore,
	)
}

// formatDirectionSection formats a section of signals grouped by confidence
func (db *DigestBuilder) formatDirectionSection(title string, signals []storage.SignalQueueRecord) string {
	var b strings.Builder
	b.WriteString(title + ":\n")
	b.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	// Group by confidence
	var high, medium, low []storage.SignalQueueRecord
	for _, s := range signals {
		switch s.SignalConfidence {
		case "high":
			high = append(high, s)
		case "medium":
			medium = append(medium, s)
		default:
			low = append(low, s)
		}
	}

	if len(high) > 0 {
		b.WriteString("🔥 HIGH CONFIDENCE:\n")
		for _, s := range high {
			b.WriteString(db.formatSignal(s))
		}
		b.WriteString("\n")
	}

	if len(medium) > 0 {
		b.WriteString("🟡 MEDIUM CONFIDENCE:\n")
		for _, s := range medium {
			b.WriteString(db.formatSignal(s))
		}
		b.WriteString("\n")
	}

	if len(low) > 0 {
		b.WriteString("⚠️ LOW CONFIDENCE:\n")
		for _, s := range low {
			b.WriteString(db.formatSignal(s))
		}
		b.WriteString("\n")
	}

	return b.String()
}

// formatSignal formats a single signal for display
func (db *DigestBuilder) formatSignal(s storage.SignalQueueRecord) string {
	freshness := ""
	if s.FundingFresh {
		freshness = " 🆕"
	}

	confidenceIcon := ""
	switch s.SignalConfidence {
	case "high":
		confidenceIcon = "🔥"
	case "medium":
		confidenceIcon = "🟡"
	default:
		confidenceIcon = "⚠️"
	}

	return fmt.Sprintf(
		"%s %s/%s: OI %s 30m (%s 2h) | Funding %.0f%% APR%s\n"+
		"  └ %s | Price %s | Score: %.0f | %s\n",
		confidenceIcon, s.Coin, s.DEX,
		formatChange(s.OIChange30m), formatChange(s.OIChange2h),
		s.FundingAPRCurrent, freshness,
		formatUSD(s.OIUSDCurrent), formatChange(s.PriceChange30m),
		s.CompositeScore, s.SignalConfidence,
	)
}

// formatTopMovers formats the top movers by 24h OI change
func (db *DigestBuilder) formatTopMovers(signals []storage.SignalQueueRecord, n int) string {
	if len(signals) == 0 {
		return ""
	}

	// Sort by 24h OI change (absolute value)
	sorted := make([]storage.SignalQueueRecord, len(signals))
	copy(sorted, signals)

	// Simple bubble sort for top N
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if math.Abs(sorted[i].OIChange24h) < math.Abs(sorted[j].OIChange24h) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	var b strings.Builder
	b.WriteString("📈 Top OI Movers (24h):\n")

	count := n
	if len(sorted) < count {
		count = len(sorted)
	}

	for i := 0; i < count; i++ {
		s := sorted[i]
		direction := "🟩"
		if s.SignalDirection == "short" {
			direction = "🟥"
		} else if s.SignalDirection == "uncertain" {
			direction = "⚪"
		}
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(fmt.Sprintf("%d. %s: %s %s", i+1, s.Coin, formatChange(s.OIChange24h), direction))
	}
	b.WriteString("\n\n")

	return b.String()
}

// formatChange formats a percentage change with sign
func formatChange(change float64) string {
	if change > 0 {
		return fmt.Sprintf("+%.1f%%", change)
	}
	return fmt.Sprintf("%.1f%%", change)
}

// formatUSD formats USD values with appropriate suffix (B, M, K)
func formatUSD(usd float64) string {
	if usd >= 1e9 {
		return fmt.Sprintf("$%.1fB", usd/1e9)
	}
	if usd >= 1e6 {
		return fmt.Sprintf("$%.1fM", usd/1e6)
	}
	return fmt.Sprintf("$%.0fK", usd/1e3)
}

// BuildInstantAlertBatch creates a single alert message for top N signals
func (db *DigestBuilder) BuildInstantAlertBatch(signals []*storage.SignalQueueRecord) string {
	if len(signals) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("🚨 TOP MOMENTUM ALERTS | %s | %d signals\n\n", time.Now().Format("15:04 UTC"), len(signals)))

	for i, s := range signals {
		direction := "🟩 LONG"
		if s.SignalDirection == "short" {
			direction = "🟥 SHORT"
		} else if s.SignalDirection == "uncertain" {
			direction = "⚪ UNCERTAIN"
		}

		freshness := ""
		if s.FundingFresh {
			freshness = " 🆕"
		}

		b.WriteString(fmt.Sprintf("%d. %s/%s | %s | Score: %.0f\n", i+1, s.Coin, s.DEX, direction, s.CompositeScore))
		b.WriteString(fmt.Sprintf("   OI: %s 30m | %s 2h | Funding: %.0f%% APR%s\n",
			formatChange(s.OIChange30m), formatChange(s.OIChange2h), s.FundingAPRCurrent, freshness))
		b.WriteString(fmt.Sprintf("   %s | Price %s | Z: %.1f\n\n",
			formatUSD(s.OIUSDCurrent), formatChange(s.PriceChange30m), s.FundingZScore))
	}

	return b.String()
}
