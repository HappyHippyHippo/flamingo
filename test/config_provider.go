package test

import (
	"fmt"

	"github.com/happyhippyhippo/flamingo"
)

type configProvider struct {
	configs []flam.ConfigSource
}

var _ flam.Provider = &configProvider{}

func (configProvider) ID() string {
	return "test.provider"
}

func (p configProvider) Reg(app flam.App) error {
	_ = app.DI().Invoke(func(cfg flam.ConfigManager) {
		for id, s := range p.configs {
			_ = cfg.AddSource(fmt.Sprintf("test.%d", id), 100, s)
		}
	})
	return nil
}
