//go:build omit_feishu

package channels

import (
	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/config"
)

type FeishuChannel struct{ disabledChannel }

const feishuCompiled = false

func NewFeishuChannel(cfg config.FeishuConfig, bus *bus.MessageBus) (*FeishuChannel, error) {
	return nil, errChannelDisabled("feishu")
}
