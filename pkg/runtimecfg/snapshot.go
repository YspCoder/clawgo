package runtimecfg

import (
	"sync/atomic"

	"github.com/YspCoder/clawgo/pkg/config"
)

var current atomic.Value // *config.Config

func Set(cfg *config.Config) {
	if cfg == nil {
		return
	}
	current.Store(cfg)
}

func Get() *config.Config {
	v := current.Load()
	if v == nil {
		return nil
	}
	cfg, _ := v.(*config.Config)
	return cfg
}
