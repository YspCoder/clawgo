//go:build omit_whatsapp

package channels

import (
	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/config"
)

type WhatsAppChannel struct{ disabledChannel }

func NewWhatsAppChannel(cfg config.WhatsAppConfig, bus *bus.MessageBus) (*WhatsAppChannel, error) {
	return nil, errChannelDisabled("whatsapp")
}
