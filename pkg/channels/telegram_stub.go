//go:build omit_telegram

package channels

import (
	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/config"
)

type TelegramChannel struct{ disabledChannel }

func NewTelegramChannel(cfg config.TelegramConfig, bus *bus.MessageBus) (*TelegramChannel, error) {
	return nil, errChannelDisabled("telegram")
}
