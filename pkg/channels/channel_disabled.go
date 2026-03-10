package channels

import (
	"context"
	"fmt"

	"github.com/YspCoder/clawgo/pkg/bus"
)

func errChannelDisabled(name string) error {
	return fmt.Errorf("%s channel is disabled at build time", name)
}

type disabledChannel struct {
	name string
}

func (c disabledChannel) Name() string {
	return c.name
}

func (c disabledChannel) Start(ctx context.Context) error {
	return errChannelDisabled(c.name)
}

func (c disabledChannel) Stop(ctx context.Context) error {
	return nil
}

func (c disabledChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	return errChannelDisabled(c.name)
}

func (c disabledChannel) IsRunning() bool {
	return false
}

func (c disabledChannel) IsAllowed(senderID string) bool {
	return false
}

func (c disabledChannel) HealthCheck(ctx context.Context) error {
	return errChannelDisabled(c.name)
}
