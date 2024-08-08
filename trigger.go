package flam

import (
	"io"
	"time"
)

// Callback @todo doc
type Callback func() error

// Trigger @todo doc
type Trigger interface {
	io.Closer
	Delay() time.Duration
}

type trigger struct {
	delay    time.Duration
	isClosed bool
	closer   func()
}

func (t trigger) Close() error {
	if !t.isClosed {
		t.isClosed = true
		t.closer()
	}
	return nil
}

func (t trigger) Delay() time.Duration {
	return t.delay
}

func newPulseTrigger(delay time.Duration, callback Callback) Trigger {
	timer := time.NewTimer(delay)
	doneCh := make(chan struct{})

	t := &trigger{
		delay:    delay,
		isClosed: false,
		closer: func() {
			timer.Stop()
		},
	}

	go func(t *trigger) {
		select {
		case <-timer.C:
			_ = callback()
		case <-doneCh:
		}
		t.closer()
		close(doneCh)
	}(t)

	return t
}

func newRecurringTrigger(delay time.Duration, callback Callback) Trigger {
	timer := time.NewTicker(delay)
	doneCh := make(chan struct{})

	t := &trigger{
		delay:    delay,
		isClosed: false,
		closer: func() {
			timer.Stop()
		},
	}

	go func(t *trigger) {
		for {
			select {
			case <-timer.C:
				if e := callback(); e != nil {
					t.closer()
					close(doneCh)
					return
				}
			case <-doneCh:
				t.closer()
				close(doneCh)
				return
			}
		}
	}(t)

	return t
}

// TriggerFactory @todo doc
type TriggerFactory interface {
	NewPulse(delay time.Duration, callback Callback) Trigger
	NewRecurring(delay time.Duration, callback Callback) Trigger
}

type triggerFactory struct{}

var _ TriggerFactory = &triggerFactory{}

func (triggerFactory) NewPulse(delay time.Duration, callback Callback) Trigger {
	return newPulseTrigger(delay, callback)
}

func (triggerFactory) NewRecurring(delay time.Duration, callback Callback) Trigger {
	return newRecurringTrigger(delay, callback)
}

type triggerProvider struct{}

func newTriggerProvider() *triggerProvider {
	return &triggerProvider{}
}

func (triggerProvider) ID() string {
	return "flam.trigger"
}

func (triggerProvider) Reg(app App) error {
	_ = app.DI().Provide(func() TriggerFactory { return &triggerFactory{} })
	return nil
}
