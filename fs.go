package flam

import (
	"github.com/spf13/afero"
)

type fsProvider struct{}

func newFsProvider() *fsProvider {
	return &fsProvider{}
}

func (fsProvider) ID() string {
	return "flam.fs"
}

func (fsProvider) Reg(app App) error {
	_ = app.DI().Provide(func() afero.Fs { return afero.NewOsFs() })
	return nil
}
