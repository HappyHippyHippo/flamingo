package test

import (
	"bytes"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	flam "github.com/happyhippyhippo/flamingo"
)

// Runner @todo doc
type Runner interface {
	App() flam.App

	ExpectPanic(expects error) Runner

	WithProcess(id string) Runner
	WithSetup(f interface{}) Runner
	WithTest(f interface{}) Runner
	WithCheck(id string, f interface{}) Runner
	WithTeardown(f interface{}) Runner

	Run() error
}

type runner struct {
	t *testing.T

	app       flam.App
	expects   error
	processes []string
	setup     interface{}
	test      interface{}
	checks    map[string]interface{}
	teardown  interface{}
}

// NewRunner @todo doc
func NewRunner(t *testing.T, app flam.App, configs ...string) (Runner, error) {
	created := &runner{
		t:      t,
		app:    app,
		checks: map[string]interface{}{},
	}
	flam.ConfigLoaderActive = false
	if e := created.loadConfigProvider(configs); e != nil {
		return nil, e
	}
	return created, nil
}

// App @todo doc
func (r *runner) App() flam.App {
	return r.app
}

// ExpectPanic @todo doc
func (r *runner) ExpectPanic(expects error) Runner {
	r.expects = expects
	return r
}

// WithProcess @todo doc
func (r *runner) WithProcess(id string) Runner {
	r.processes = append(r.processes, id)
	return r
}

// WithSetup @todo doc
func (r *runner) WithSetup(f interface{}) Runner {
	r.setup = f
	return r
}

// WithTest @todo doc
func (r *runner) WithTest(f interface{}) Runner {
	r.test = f
	return r
}

// WithCheck @todo doc
func (r *runner) WithCheck(id string, f interface{}) Runner {
	r.checks[id] = f
	return r
}

// WithTeardown @todo doc
func (r *runner) WithTeardown(f interface{}) Runner {
	r.teardown = f
	return r
}

// Run @todo doc
func (r *runner) Run() error {
	_ = r.app.DI().Invoke(func(kennel flam.WatchdogKennel) {
		_ = kennel.DisableProcess("rest")
		for _, id := range r.processes {
			_ = kennel.EnableProcess(id)
		}
	})

	wgBooted := sync.WaitGroup{}
	wgRunning := sync.WaitGroup{}
	wgBooted.Add(1)
	wgRunning.Add(1)
	go func() {
		_ = r.app.Boot()
		wgBooted.Done()
		_ = r.app.Run()
		wgRunning.Done()
	}()
	wgBooted.Wait()
	time.Sleep(10 * time.Millisecond)

	defer func() {
		wgRunning.Wait()
		if r.expects != nil {
			err := recover()
			if err == nil {
				r.t.Error("didn't panic")
			} else if e, ok := err.(error); !ok {
				r.t.Error("didn't panic with an error")
			} else if !errors.Is(e, r.expects) {
				r.t.Errorf("unexpected error : %+v, when expecting %+v", e, r.expects)
			}
		}
		r.runChecks()
		_ = r.runTeardown()
		_ = r.app.Close()
	}()
	if e := r.runSetup(); e != nil {
		return e
	}
	return r.runExecute()
}

func (r *runner) loadConfigProvider(configs []string) error {
	p := &configProvider{}
	for _, file := range configs {
		c, e := r.loadConfig(file)
		if e != nil {
			return e
		}
		p.configs = append(p.configs, c)
	}
	_ = r.app.Provide(p)
	return nil
}

func (r *runner) loadConfig(file string) (flam.ConfigSource, error) {
	data, e := os.ReadFile(file)
	if e != nil {
		return nil, e
	}
	// parse file yaml
	m := flam.Bag{}
	if e := yaml.NewDecoder(bytes.NewBuffer(data)).Decode(&m); e != nil {
		return nil, e
	}
	return &m, nil
}

func (r *runner) runSetup() error {
	if r.setup != nil {
		if e := r.app.DI().Invoke(r.setup); e != nil {
			r.t.Errorf("error running test setup : %v", e)
		}
	}
	return nil
}

func (r *runner) runExecute() error {
	if r.test != nil {
		if e := r.app.DI().Invoke(r.test); e != nil {
			r.t.Errorf("error running test : %v", e)
		}
	}
	return nil
}

func (r *runner) runChecks() {
	for id, check := range r.checks {
		r.t.Run(id, func(t *testing.T) {
			_ = r.app.DI().Invoke(check)
		})
	}
	r.checks = map[string]interface{}{}
}

func (r *runner) runTeardown() error {
	if r.teardown != nil {
		if e := r.app.DI().Invoke(r.teardown); e != nil {
			r.t.Errorf("error running test teardown : %v", e)
		}
	}
	return nil
}
