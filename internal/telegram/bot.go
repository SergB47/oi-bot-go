package telegram

import (
	"fmt"
	"log"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// RateLimiter implements token bucket rate limiting
type RateLimiter struct {
	tokens   chan struct{}
	interval time.Duration
}

// NewRateLimiter creates a new rate limiter with specified rate
func NewRateLimiter(rate int, per time.Duration) *RateLimiter {
	rl := &RateLimiter{
		tokens:   make(chan struct{}, rate),
		interval: per,
	}
	for i := 0; i < rate; i++ {
		rl.tokens <- struct{}{}
	}
	go rl.refill(rate, per)
	return rl
}

func (rl *RateLimiter) refill(rate int, per time.Duration) {
	ticker := time.NewTicker(per / time.Duration(rate))
	defer ticker.Stop()
	for range ticker.C {
		select {
		case rl.tokens <- struct{}{}:
		default:
		}
	}
}

// Wait blocks until a token is available
func (rl *RateLimiter) Wait() {
	<-rl.tokens
}

// Bot represents a Telegram bot for sending alerts
type Bot struct {
	api         *tgbotapi.BotAPI
	chatID      int64
	enabled     bool
	rateLimiter *RateLimiter
}

// NewBot creates a new Telegram bot instance
func NewBot(token string, chatID int64) (*Bot, error) {
	if token == "" {
		log.Println("Telegram bot token not provided, bot disabled")
		return &Bot{enabled: false}, nil
	}

	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	log.Printf("Telegram bot authorized: @%s", api.Self.UserName)

	return &Bot{
		api:         api,
		chatID:      chatID,
		enabled:     true,
		rateLimiter: NewRateLimiter(20, time.Minute), // 20 msg/min
	}, nil
}

// SendFundingAlert sends an alert about significant funding rate change
func (b *Bot) SendFundingAlert(coin, dex string, oldFunding, newFunding, changePercent float64, period string) error {
	if !b.enabled {
		return nil
	}

	direction := "📈 UP"
	if newFunding < oldFunding {
		direction = "📉 DOWN"
	}

	msg := fmt.Sprintf(
		"🚨 *Funding Alert*\n\n"+
		"*Asset:* %s/%s\n"+
		"*Period:* %s\n"+
		"*Direction:* %s\n"+
		"*Change:* %.2f%%\n\n"+
		"*Previous:* %.8f\n"+
		"*Current:* %.8f\n\n"+
		"*Old APR:* %.2f%%\n"+
		"*New APR:* %.2f%%",
		coin, dex, period, direction, changePercent,
		oldFunding, newFunding,
		oldFunding*24*365*100, newFunding*24*365*100,
	)

	return b.sendMessage(msg)
}

// SendAlert sends a generic alert message
func (b *Bot) SendAlert(message string) error {
	if !b.enabled {
		return nil
	}
	return b.sendMessage(message)
}

func (b *Bot) sendMessage(text string) error {
	if !b.enabled {
		return nil
	}

	b.rateLimiter.Wait() // Respect rate limit

	msg := tgbotapi.NewMessage(b.chatID, text)
	msg.ParseMode = tgbotapi.ModeMarkdown

	// Retry with exponential backoff
	var err error
	for i := 0; i < 3; i++ {
		_, err = b.api.Send(msg)
		if err == nil {
			return nil
		}

		// Check if rate limited (429 error)
		if strings.Contains(err.Error(), "429") ||
			strings.Contains(err.Error(), "Too Many Requests") {
			backoff := time.Duration(i+1) * 2 * time.Second
			time.Sleep(backoff)
			continue
		}

		return fmt.Errorf("failed to send message: %w", err)
	}

	return fmt.Errorf("failed to send message after retries: %w", err)
}

// IsEnabled returns whether the bot is enabled
func (b *Bot) IsEnabled() bool {
	return b.enabled
}
