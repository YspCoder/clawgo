//go:build omit_dingtalk

package channels

import (
	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/config"
)

type DingTalkChannel struct{ disabledChannel }

const dingtalkCompiled = false

func NewDingTalkChannel(cfg config.DingTalkConfig, bus *bus.MessageBus) (*DingTalkChannel, error) {
	return nil, errChannelDisabled("dingtalk")
}
