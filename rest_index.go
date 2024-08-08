package flam

import (
	"net/http"

	"github.com/gorilla/mux"
	"go.uber.org/dig"
)

type restIndexGetResponse struct {
	App     string `json:"app"`
	Version string `json:"version"`
}

type restIndexService struct {
	app     string
	version string
}

func newRestIndexService(config Config) *restIndexService {
	return &restIndexService{
		app:     config.String("app.name", ""),
		version: config.String("app.version", ""),
	}
}

func (s *restIndexService) Get() restIndexGetResponse {
	return restIndexGetResponse{
		App:     s.app,
		Version: s.version,
	}
}

type restIndexRegister struct {
	RestRegister

	serviceID         int
	envelopeGenerator EnvelopeRestMwGenerator
	logGenerator      LogRestMwGenerator
	service           *restIndexService
}

var _ RestRegister = &restIndexRegister{}

func newRestIndexRegister(
	config Config,
	validator Validator,
	router *mux.Router,
	envelopeGenerator EnvelopeRestMwGenerator,
	logGenerator LogRestMwGenerator,
	service *restIndexService,
) RestRegister {
	return &restIndexRegister{
		RestRegister:      NewRestRegister(config, validator, router),
		serviceID:         config.Int(RestServiceConfigPath, 0),
		envelopeGenerator: envelopeGenerator,
		logGenerator:      logGenerator,
		service:           service,
	}
}

func (r *restIndexRegister) Reg() error {
	var handler http.Handler = http.HandlerFunc(r.handleGet)

	lmw := r.logGenerator(http.StatusOK)
	emw := r.envelopeGenerator(r.serviceID, 1, http.StatusOK)
	handler = lmw(emw(handler))

	return r.RegEndpoint(
		handler,
		Bag{
			"path":   "/",
			"method": "get"})
}

func (r *restIndexRegister) handleGet(w http.ResponseWriter, _ *http.Request) {
	_ = r.Write(w, r.service.Get())
}

// NewRestIndexProvider @todo doc
func NewRestIndexProvider() Provider {
	return &restIndexProvider{}
}

type restIndexProvider struct{}

var _ Provider = &restIndexProvider{}

func (p *restIndexProvider) ID() string {
	return "flam.rest.index"
}

func (p *restIndexProvider) Reg(app App) error {
	_ = app.DI().Provide(newRestIndexService)
	_ = app.DI().Provide(newRestIndexRegister, dig.Group(RestRegisterTag))
	return nil
}
