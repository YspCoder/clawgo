//go:build omit_weixin

package channels

import (
	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/config"
)

type WeixinChannel struct{ disabledChannel }

const weixinCompiled = false

func NewWeixinChannel(cfg config.WeixinConfig, bus *bus.MessageBus) (*WeixinChannel, error) {
	return nil, errChannelDisabled("weixin")
}
