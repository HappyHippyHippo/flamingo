package flam

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/gorilla/mux"
	"go.uber.org/dig"
)

const (
	// RestRegisterTag @todo doc
	RestRegisterTag = "flam.rest.register"

	// RestServerCreatorTag @todo doc
	RestServerCreatorTag = "flam.rest.server.creator"
)

var (
	// RestConfigPath @todo doc
	RestConfigPath = EnvString("FLAMINGO_REST_CONFIG_PATH", "flam.rest")

	// RestServiceConfigPath @todo doc
	RestServiceConfigPath = EnvString("FLAMINGO_REST_SERVICE_CONFIG_PATH", "flam.rest.service")

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

func newRestConfigTLS() *tls.Config {
	return &tls.Config{
		MinVersion:       tls.VersionTLS12,
		CurvePreferences: []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_RSA_WITH_AES_256_CBC_SHA,
		},
	}
}

// RestServer @todo doc
type RestServer interface {
	Run() error
}

// RestServerCreator @todo doc
type RestServerCreator interface {
	Accept(config *Bag) bool
	Create(config *Bag) (RestServer, error)
}

// RestServerFactory @todo doc
type RestServerFactory interface {
	AddCreator(creator RestServerCreator) RestServerFactory
	Create(config *Bag) (RestServer, error)
}

type restServerFactory []RestServerCreator

var _ RestServerFactory = &restServerFactory{}

func newRestServerFactory(
	args struct {
		dig.In
		Creators []RestServerCreator `group:"flam.rest.server.creator"`
	},
) RestServerFactory {
	factory := &restServerFactory{}
	for _, creator := range args.Creators {
		*factory = append(*factory, creator)
	}
	return factory
}

func (f *restServerFactory) AddCreator(creator RestServerCreator) RestServerFactory {
	*f = append(*f, creator)
	return f
}

func (f *restServerFactory) Create(config *Bag) (RestServer, error) {
	for _, creator := range *f {
		if creator.Accept(config) {
			return creator.Create(config)
		}
	}
	return nil, NewError("unable to parse server config", Bag{"config": config})
}

type restServer struct {
	log        RestLogAdapter
	handler    http.Handler
	watchdogID string
	port       int
	termCh     chan os.Signal
}

var _ RestServer = &restServer{}

func (p *restServer) Run() error {
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
	p.awaitTermination(srv)
	return nil
}

func (p *restServer) awaitTermination(srv *http.Server) {
	signal.Notify(p.termCh, syscall.SIGINT, syscall.SIGKILL, syscall.SIGTERM)
	_ = <-p.termCh
	_ = p.log.ShutdownGracefully(p.watchdogID)
	if e := srv.Shutdown(context.Background()); e != nil {
		_ = p.log.ShutdownError(p.watchdogID, e)
	}
}

type restServerCreator struct {
	log     RestLogAdapter
	handler http.Handler
	config  struct {
		Type       string
		WatchdogID string
		Port       int
	}
}

var _ RestServerCreator = &restServerCreator{}

func newRestServerCreator(log RestLogAdapter, handler http.Handler) RestServerCreator {
	return &restServerCreator{
		log:     log,
		handler: handler,
	}
}

func (sc *restServerCreator) Accept(config *Bag) bool {
	sc.config.Type = ""
	sc.config.WatchdogID = ""
	sc.config.Port = 0
	if e := config.Populate("", &sc.config); e != nil {
		return false
	}
	return strings.ToLower(sc.config.Type) == "default" &&
		sc.config.WatchdogID != "" &&
		sc.config.Port != 0
}

func (sc *restServerCreator) Create(_ *Bag) (RestServer, error) {
	return &restServer{
		log:        sc.log,
		handler:    sc.handler,
		watchdogID: sc.config.WatchdogID,
		port:       sc.config.Port,
		termCh:     make(chan os.Signal, 1),
	}, nil
}

type restServerTLS struct {
	restServer
	config *tls.Config
	cert   string
	key    string
}

var _ RestServer = &restServerTLS{}

func (p *restServerTLS) Run() error {
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", p.port),
		Handler:      p.handler,
		TLSConfig:    p.config,
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler), 0),
	}
	go func() {
		_ = p.log.Start(p.watchdogID)
		if e := srv.ListenAndServeTLS(p.cert, p.key); e != nil && !errors.Is(e, http.ErrServerClosed) {
			_ = p.log.Error(p.watchdogID, e)
		}
		_ = p.log.Done(p.watchdogID)
	}()
	p.awaitTermination(srv)
	return nil
}

type restServerTLSCreator struct {
	log       RestLogAdapter
	handler   http.Handler
	tlsConfig *tls.Config
	config    struct {
		Type       string
		WatchdogID string
		Port       int
		TLS        struct {
			Cert string
			Key  string
		}
	}
}

var _ RestServerCreator = &restServerTLSCreator{}

func newRestServerTLSCreator(log RestLogAdapter, handler http.Handler, tlsConfig *tls.Config) RestServerCreator {
	return &restServerTLSCreator{
		log:       log,
		handler:   handler,
		tlsConfig: tlsConfig,
	}
}

func (sc *restServerTLSCreator) Accept(config *Bag) bool {
	sc.config.Type = ""
	sc.config.WatchdogID = ""
	sc.config.Port = 0
	sc.config.TLS.Cert = ""
	sc.config.TLS.Key = ""
	if e := config.Populate("", &sc.config); e != nil {
		return false
	}
	return strings.ToLower(sc.config.Type) == "tls" &&
		sc.config.WatchdogID != "" &&
		sc.config.Port != 0 &&
		sc.config.TLS.Cert != "" &&
		sc.config.TLS.Key != ""
}

func (sc *restServerTLSCreator) Create(_ *Bag) (RestServer, error) {
	return &restServerTLS{
		restServer: restServer{
			log:        sc.log,
			handler:    sc.handler,
			watchdogID: sc.config.WatchdogID,
			port:       sc.config.Port,
			termCh:     make(chan os.Signal, 1),
		},
		config: sc.tlsConfig,
		cert:   sc.config.TLS.Cert,
		key:    sc.config.TLS.Key,
	}, nil
}

type restProcess struct {
	factory RestServerFactory
	server  RestServer
	config  *Bag
}

var _ WatchdogProcess = &restProcess{}

func newRestProcess(config Config, factory RestServerFactory) WatchdogProcess {
	return &restProcess{
		factory: factory,
		config:  config.Bag(RestConfigPath, nil),
	}
}

func (p *restProcess) ID() string {
	return p.config.String("WatchdogID", "rest")
}

func (p *restProcess) Run() error {
	server, e := p.factory.Create(p.config)
	if e != nil {
		return e
	}
	p.server = server

	return server.Run()
}

func (p *restProcess) Terminate() error {
	server, ok := p.server.(*restServer)
	if !ok {
		return nil
	}

	server.termCh <- syscall.SIGTERM
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
	_ = app.DI().Provide(newRestConfigTLS)
	_ = app.DI().Provide(newRestServerFactory)
	_ = app.DI().Provide(newRestServerCreator, dig.Group(RestServerCreatorTag))
	_ = app.DI().Provide(newRestServerTLSCreator, dig.Group(RestServerCreatorTag))
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
