package telegram

import (
	"fmt"
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Bot represents a Telegram bot for sending alerts
type Bot struct {
	api       *tgbotapi.BotAPI
	chatID    int64
	enabled  bool
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
		api:     api,
		chatID:  chatID,
		enabled: true,
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
	msg := tgbotapi.NewMessage(b.chatID, text)
	msg.ParseMode = tgbotapi.ModeMarkdown

	_, err := b.api.Send(msg)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	return nil
}

// IsEnabled returns whether the bot is enabled
func (b *Bot) IsEnabled() bool {
	return b.enabled
}
