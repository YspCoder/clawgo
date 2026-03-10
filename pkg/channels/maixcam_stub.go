//go:build omit_maixcam

package channels

import (
	"github.com/YspCoder/clawgo/pkg/bus"
	"github.com/YspCoder/clawgo/pkg/config"
)

type MaixCamChannel struct{ disabledChannel }

func NewMaixCamChannel(cfg config.MaixCamConfig, bus *bus.MessageBus) (*MaixCamChannel, error) {
	return nil, errChannelDisabled("maixcam")
}
