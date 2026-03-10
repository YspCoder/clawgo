//go:build omit_qq

package channels

import (
	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/config"
)

type QQChannel struct{ disabledChannel }

func NewQQChannel(cfg config.QQConfig, bus *bus.MessageBus) (*QQChannel, error) {
	return nil, errChannelDisabled("qq")
}
