package flam

import (
	"io"

	"go.uber.org/dig"
)

// Provider @todo doc
type Provider interface {
	ID() string
	Reg(a App) error
}

// Bootable @todo doc
type Bootable interface {
	Boot() error
}

// App @todo doc
type App interface {
	io.Closer

	DI() *dig.Container
	Provide(p Provider) error
	Boot() error
	Run() error
}

// NewApp @todo doc
func NewApp() App {
	a := &app{
		di:        dig.New(),
		providers: []Provider{},
		isBooted:  false,
	}

	_ = a.Provide(newFsProvider())
	_ = a.Provide(newTriggerProvider())
	_ = a.Provide(newConfigProvider())
	_ = a.Provide(newLogProvider())
	_ = a.Provide(newRdbProvider())
	_ = a.Provide(newMigrationProvider())
	_ = a.Provide(newValidationProvider())
	_ = a.Provide(newRestProvider())
	_ = a.Provide(newEnvelopeRestMwProvider())
	_ = a.Provide(newLogRestMwProvider())
	_ = a.Provide(newWatchdogProvider())

	return a
}

type app struct {
	di        *dig.Container
	providers []Provider
	isBooted  bool
}

func (a *app) Close() error {
	for _, p := range a.providers {
		if closer, ok := p.(io.Closer); ok {
			if e := closer.Close(); e != nil {
				return e
			}
		}
	}
	return nil
}

func (a *app) DI() *dig.Container {
	return a.di
}

func (a *app) Provide(p Provider) error {
	id := p.ID()
	for _, r := range a.providers {
		if r.ID() == id {
			return NewError("duplicate provider", Bag{"id": id})
		}
	}
	a.providers = append(a.providers, p)
	return p.Reg(a)
}

func (a *app) Boot() error {
	if a.isBooted {
		return nil
	}
	for _, p := range a.providers {
		if bootable, ok := p.(Bootable); ok {
			if e := bootable.Boot(); e != nil {
				return e
			}
		}
	}
	a.isBooted = true
	return nil
}

// Run @todo doc
func (a *app) Run() error {
	if e := a.Boot(); e != nil {
		return e
	}
	if e := a.di.Invoke(
		func(k WatchdogKennel) error {
			return k.Run()
		},
	); e != nil {
		return e
	}
	return nil
}
