package flam

import (
	"sync"

	"go.uber.org/dig"
)

const (
	// WatchdogProcessTag @todo doc
	WatchdogProcessTag = "flam.watchdog.process"
)

// WatchdogLogAdapter @todo doc
type WatchdogLogAdapter interface {
	Start(id string) error
	Error(id string, e error) error
	Done(id string) error
}

type watchdogLogAdapter struct {
	logger Logger
}

var _ WatchdogLogAdapter = &watchdogLogAdapter{}

func newWatchdogLogAdapter(logger Logger) WatchdogLogAdapter {
	return &watchdogLogAdapter{
		logger: logger,
	}
}

func (la watchdogLogAdapter) Start(id string) error {
	return la.logger.Signal(LogInfo, "watchdog", "Watchdog starting ...", Bag{"watchdog": id})
}

func (la watchdogLogAdapter) Error(id string, e error) error {
	return la.logger.Signal(LogError, "watchdog", "Watchdog error", Bag{"watchdog": id, "error": e})
}

func (la watchdogLogAdapter) Done(id string) error {
	return la.logger.Signal(LogInfo, "watchdog", "Watchdog terminated", Bag{"watchdog": id})
}

type watchdog struct {
	logAdapter WatchdogLogAdapter
}

// WatchdogProcess @todo doc
type WatchdogProcess interface {
	ID() string
	Run() error
	Terminate() error
}

func (w *watchdog) Run(process WatchdogProcess) error {
	var panicErr error
	var e error
	runner := func() error {
		defer func() {
			if resp := recover(); resp != nil {
				panicErr = resp.(error)
			}
		}()
		return process.Run()
	}
	_ = w.logAdapter.Start(process.ID())
	for {
		e = runner()
		if panicErr != nil {
			_ = w.logAdapter.Error(process.ID(), panicErr)
			panicErr = nil
			continue
		}
		break
	}
	_ = w.logAdapter.Done(process.ID())
	return e
}

type watchdogFactory struct {
	adapter WatchdogLogAdapter
}

func newWatchdogFactory(adapter WatchdogLogAdapter) *watchdogFactory {
	return &watchdogFactory{
		adapter: adapter,
	}
}

func (f *watchdogFactory) create() *watchdog {
	return &watchdog{
		logAdapter: f.adapter,
	}
}

// WatchdogProcessEnabler @todo doc
type WatchdogProcessEnabler func(id string) bool

type watchdogKennelReg struct {
	process  WatchdogProcess
	watchdog *watchdog
}

type watchdogKennel struct {
	enabler WatchdogProcessEnabler
	factory *watchdogFactory
	regs    map[string]watchdogKennelReg
}

// WatchdogKennel @todo doc
type WatchdogKennel interface {
	SetEnabler(enabler WatchdogProcessEnabler) WatchdogKennel
	AddProcess(process WatchdogProcess) error
	GetProcess(id string) (WatchdogProcess, error)
	Run() error
}

var _ WatchdogKennel = &watchdogKennel{}

func newWatchdogKennel(
	args struct {
		dig.In
		Factory   *watchdogFactory
		Processes []WatchdogProcess `group:"flam.watchdog.process"`
	},
) (WatchdogKennel, error) {
	k := &watchdogKennel{
		factory: args.Factory,
		regs:    map[string]watchdogKennelReg{},
	}
	for _, p := range args.Processes {
		if e := k.AddProcess(p); e != nil {
			return nil, e
		}
	}
	return k, nil
}

func (k *watchdogKennel) SetEnabler(enabler WatchdogProcessEnabler) WatchdogKennel {
	k.enabler = enabler
	return k
}

func (k *watchdogKennel) AddProcess(process WatchdogProcess) error {
	id := process.ID()
	if _, ok := k.regs[id]; ok {
		return NewError("duplicate process", Bag{"id": id})
	}
	k.regs[id] = watchdogKennelReg{
		process:  process,
		watchdog: k.factory.create(),
	}
	return nil
}

func (k *watchdogKennel) GetProcess(id string) (WatchdogProcess, error) {
	p, ok := k.regs[id]
	if !ok {
		return nil, NewError("process not found", Bag{"id": id})
	}
	return p.process, nil
}

func (k *watchdogKennel) Run() error {
	if len(k.regs) == 0 {
		return nil
	}
	var result error
	wg := sync.WaitGroup{}
	for id, reg := range k.regs {
		if k.enabler != nil && !k.enabler(id) {
			continue
		}
		wg.Add(1)
		go func(reg watchdogKennelReg) {
			defer wg.Done()
			result = reg.watchdog.Run(reg.process)
		}(reg)
	}
	wg.Wait()
	return result
}

type watchdogProvider struct{}

var _ Provider = &watchdogProvider{}

func newWatchdogProvider() Provider {
	return &watchdogProvider{}
}

func (*watchdogProvider) ID() string {
	return "flam.watchdog"
}

func (p *watchdogProvider) Reg(app App) error {
	_ = app.DI().Provide(newWatchdogLogAdapter)
	_ = app.DI().Provide(newWatchdogFactory)
	_ = app.DI().Provide(newWatchdogKennel)
	return nil
}
