package flam

import (
	"net/http"

	"github.com/gorilla/mux"
	"go.uber.org/dig"
	"gorm.io/gorm"
)

const (
	// RestHealthTesterTag @todo doc
	RestHealthTesterTag = "flam.rest.health.tester"
)

// RestHealthTester @todo doc
type RestHealthTester interface {
	ID() string
	Run() string
}

// NewRestHealthDatabaseTester @todo doc
func NewRestHealthDatabaseTester(id string, conn *gorm.DB) RestHealthTester {
	return &databaseTester{
		id:   id,
		conn: conn,
	}
}

type databaseTester struct {
	id   string
	conn *gorm.DB
}

var _ RestHealthTester = &databaseTester{}

func (dt *databaseTester) ID() string {
	return dt.id
}

func (dt *databaseTester) Run() string {
	db, e := dt.conn.DB()
	if e != nil {
		return e.Error()
	}
	if e = db.Ping(); e != nil {
		return e.Error()
	}
	return "OK"
}

type restHealthGetResponse struct {
	Status map[string]string `json:"status"`
}

type restHealthService struct {
	testers []RestHealthTester
}

func newRestHealthService(
	args struct {
		dig.In
		Testers []RestHealthTester `group:"flam.rest.health.tester"`
	},
) *restHealthService {
	return &restHealthService{
		testers: args.Testers,
	}
}

func (s *restHealthService) Get() restHealthGetResponse {
	r := restHealthGetResponse{Status: map[string]string{}}
	for _, tester := range s.testers {
		r.Status[tester.ID()] = tester.Run()
	}
	return r
}

type restHealthRegister struct {
	RestRegister

	serviceID         int
	envelopeGenerator EnvelopeRestMwGenerator
	logGenerator      LogRestMwGenerator
	service           *restHealthService
}

var _ RestRegister = &restHealthRegister{}

func newRestHealthRegister(
	config Config,
	validator Validator,
	router *mux.Router,
	envelopeGenerator EnvelopeRestMwGenerator,
	logGenerator LogRestMwGenerator,
	service *restHealthService,
) RestRegister {
	return &restHealthRegister{
		RestRegister:      NewRestRegister(config, validator, router),
		serviceID:         config.Int(RestServiceConfigPath, 0),
		envelopeGenerator: envelopeGenerator,
		logGenerator:      logGenerator,
		service:           service,
	}
}

func (r *restHealthRegister) Reg() error {
	var handler http.Handler = http.HandlerFunc(r.handleGet)

	lmw := r.logGenerator(http.StatusOK)
	emw := r.envelopeGenerator(r.serviceID, 2, http.StatusOK)
	handler = lmw(emw(handler))

	return r.RegEndpoint(
		handler,
		Bag{
			"path":   "/health",
			"method": "get"})
}

func (r *restHealthRegister) handleGet(w http.ResponseWriter, _ *http.Request) {
	_ = r.Write(w, r.service.Get())
}

// NewRestHealthProvider @todo doc
func NewRestHealthProvider() Provider {
	return &restHealthProvider{}
}

type restHealthProvider struct{}

var _ Provider = &restHealthProvider{}

func (p *restHealthProvider) ID() string {
	return "flam.rest.health"
}

func (p *restHealthProvider) Reg(app App) error {
	_ = app.DI().Provide(newRestHealthService)
	_ = app.DI().Provide(newRestHealthRegister, dig.Group(RestRegisterTag))
	return nil
}
