package flam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gorilla/mux"
	"go.uber.org/dig"
)

const (
	// RestRegisterTag @todo doc
	RestRegisterTag = "flam.rest.register"
)

var (
	// RestConfigPath @todo doc
	RestConfigPath = EnvString("FLAMINGO_REST_CONFIG_PATH", "flam.rest")

	// RestServiceConfigPath @todo doc
	RestServiceConfigPath = EnvString("FLAMINGO_REST_SERVICE_CONFIG_PATH", "flam.rest.service")

	// RestEndpointConfigPath @todo doc
	RestEndpointConfigPath = EnvString("FLAMINGO_REST_ENDPOINT_CONFIG_PATH", "flam.rest.endpoints.%s")

	// RestPort @todo doc
	RestPort = EnvInt("FLAMINGO_REST_PORT", 80)
)

// RestLogAdapter @todo doc
type RestLogAdapter interface {
	WatchdogLogAdapter

	ShutdownGracefully(id string) error
	ShutdownError(id string, e error) error
}

type restLogAdapter struct {
	logger Logger
}

var _ RestLogAdapter = &restLogAdapter{}

func newRestLogAdapter(logger Logger) RestLogAdapter {
	return &restLogAdapter{
		logger: logger,
	}
}

func (r restLogAdapter) Start(id string) error {
	return r.logger.Signal(LogInfo, "rest", "Starting rest watchdog ...", Bag{"watchdog": id})
}

func (r restLogAdapter) Error(id string, e error) error {
	return r.logger.Signal(LogError, "rest", "Rest watchdog error", Bag{"watchdog": id, "error": e.Error()})
}

func (r restLogAdapter) Done(id string) error {
	return r.logger.Signal(LogInfo, "rest", "Rest watchdog terminated", Bag{"watchdog": id})
}

func (r restLogAdapter) ShutdownGracefully(id string) error {
	return r.logger.Signal(LogInfo, "rest", "Rest watchdog gracefully shut down", Bag{"watchdog": id})
}

func (r restLogAdapter) ShutdownError(id string, e error) error {
	return r.logger.Signal(LogError, "rest", "Error shutting down rest watchdog", Bag{"watchdog": id, "error": e.Error()})
}

// RestRegister @todo doc
type RestRegister interface {
	Config() Config
	Validator() Validator
	Router() *mux.Router

	Reg() error
	RegEndpoint(handler http.Handler, config Bag) error
	Validate(data interface{})
	Write(writer http.ResponseWriter, data interface{}) error
}

// NewRestRegister @todo dc
func NewRestRegister(config Config, validator Validator, router *mux.Router) RestRegister {
	return &restRegister{
		config:    config,
		validator: validator,
		router:    router,
	}
}

type restRegister struct {
	config    Config
	validator Validator
	router    *mux.Router
}

var _ RestRegister = &restRegister{}

func (r restRegister) Config() Config {
	return r.config
}

func (r restRegister) Validator() Validator {
	return r.validator
}

func (r restRegister) Router() *mux.Router {
	return r.router
}

func (r restRegister) Reg() error {
	return nil
}

func (r restRegister) RegEndpoint(handler http.Handler, config Bag) error {
	r.router.
		Handle(config.String("path", "/"), handler).
		Methods(config.String("method", "get"))
	return nil
}

func (r restRegister) Validate(data interface{}) {
	if env := r.validator(data); env != nil {
		panic(env)
	}
}

func (r restRegister) Write(writer http.ResponseWriter, data interface{}) error {
	if w, ok := writer.(EnvelopeWriter); ok {
		if e, ok := data.(Envelope); ok {
			return w.WriteEnvelope(e)
		}
		return w.WriteEnvelope(NewEnvelope(data))
	}
	bytes, e := json.Marshal(data)
	if e != nil {
		return e
	}
	_, e = writer.Write(bytes)
	return e
}

type restProcess struct {
	log        RestLogAdapter
	handler    http.Handler
	watchdogID string
	port       int
	termCh     chan os.Signal
}

var _ WatchdogProcess = &restProcess{}

func newRestProcess(config Config, logAdapter RestLogAdapter, handler http.Handler) WatchdogProcess {
	c := struct {
		Watchdog string
		Port     int
	}{
		Watchdog: "flam.process.rest",
		Port:     RestPort,
	}
	_ = config.Populate(RestConfigPath, &c)
	return &restProcess{
		log:        logAdapter,
		handler:    handler,
		watchdogID: c.Watchdog,
		port:       c.Port,
		termCh:     make(chan os.Signal, 1),
	}
}

func (p *restProcess) ID() string {
	return p.watchdogID
}

func (p *restProcess) Run() error {
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", p.port),
		Handler: p.handler,
	}
	go func() {
		_ = p.log.Start(p.watchdogID)
		if e := srv.ListenAndServe(); e != nil && !errors.Is(e, http.ErrServerClosed) {
			_ = p.log.Error(p.watchdogID, e)
		}
		_ = p.log.Done(p.watchdogID)
	}()
	signal.Notify(p.termCh, syscall.SIGINT, syscall.SIGKILL, syscall.SIGTERM)
	sig := <-p.termCh
	_ = sig
	_ = p.log.ShutdownGracefully(p.watchdogID)
	if e := srv.Shutdown(context.Background()); e != nil {
		_ = p.log.ShutdownError(p.watchdogID, e)
	}
	return nil
}

func (p *restProcess) Terminate() error {
	p.termCh <- syscall.SIGTERM
	return nil
}

type restLoader struct {
	registers []RestRegister
}

func newRestLoader(
	args struct {
		dig.In
		Registers []RestRegister `group:"flam.rest.register"`
	},
) *restLoader {
	return &restLoader{
		registers: args.Registers,
	}
}

func (l *restLoader) load() error {
	for _, r := range l.registers {
		if e := r.Reg(); e != nil {
			return e
		}
	}
	return nil
}

type restInitializer struct {
	loader *restLoader
}

func newRestInitializer(loader *restLoader) *restInitializer {
	return &restInitializer{
		loader: loader,
	}
}

func (i *restInitializer) init() error {
	return i.loader.load()
}

func (i *restInitializer) close() error {
	return nil
}

type restProvider struct {
	app App
}

var _ Provider = &restProvider{}

func newRestProvider() Provider {
	return &restProvider{}
}

func (*restProvider) ID() string {
	return "flam.rest"
}

func (p *restProvider) Reg(app App) error {
	_ = app.DI().Provide(mux.NewRouter)
	_ = app.DI().Provide(func(handler *mux.Router) http.Handler { return handler })
	_ = app.DI().Provide(newRestLogAdapter)
	_ = app.DI().Provide(newRestProcess, dig.Group(WatchdogProcessTag))
	_ = app.DI().Provide(newRestLoader)
	_ = app.DI().Provide(newRestInitializer)
	p.app = app
	return nil
}

func (p *restProvider) Boot() error {
	return p.app.DI().Invoke(func(i *restInitializer) error {
		return i.init()
	})
}

func (p *restProvider) Close() error {
	return p.app.DI().Invoke(func(i *restInitializer) error {
		return i.close()
	})
}
