package flam

import (
	"fmt"
	"io"
	"strings"

	"go.uber.org/dig"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	// RdbDialectTag @todo doc
	RdbDialectTag = "flam.rdb.dialect"
)

var (
	// RdbPrimaryConnName @todo doc
	RdbPrimaryConnName = EnvString("FLAMINGO_RDB_PRIMARY_CONN", "primary")

	// RdbConfigPath @todo doc
	RdbConfigPath = EnvString("FLAMINGO_RDB_CONN_CONFIG_PATH", "flam.rdb")
)

// RdbDialectCreator @todo doc
type RdbDialectCreator interface {
	Accept(config *Bag) bool
	Create(config *Bag) (gorm.Dialector, error)
}

// RdbDialectFactory @todo doc
type RdbDialectFactory interface {
	AddCreator(creator RdbDialectCreator) RdbDialectFactory
	Create(config *Bag) (gorm.Dialector, error)
}

var rdbDialectCreators []func() RdbDialectCreator

type rdbDialectFactory []RdbDialectCreator

var _ RdbDialectFactory = &rdbDialectFactory{}

func newRdbDialectFactory(
	args struct {
		dig.In
		Creators []RdbDialectCreator `group:"flam.rdb.dialect"`
	},
) *rdbDialectFactory {
	factory := &rdbDialectFactory{}
	for _, creator := range args.Creators {
		*factory = append(*factory, creator)
	}
	return factory
}

func (f *rdbDialectFactory) AddCreator(creator RdbDialectCreator) RdbDialectFactory {
	*f = append(*f, creator)
	return f
}

func (f *rdbDialectFactory) Create(config *Bag) (gorm.Dialector, error) {
	for _, creator := range *f {
		if creator.Accept(config) {
			return creator.Create(config)
		}
	}
	return nil, NewError("unable to parse dialect config", Bag{"config": config})
}

type sqliteRdbDialectCreator struct {
	config struct {
		Dialect string
		Host    string
		Params  Bag
	}
}

var _ RdbDialectCreator = &sqliteRdbDialectCreator{}

func (dc *sqliteRdbDialectCreator) Accept(config *Bag) bool {
	dc.config.Host = "localhost"
	if e := config.Populate("", &dc.config); e != nil {
		return false
	}
	return strings.ToLower(dc.config.Dialect) == "sqlite" &&
		dc.config.Host != ""
}

func (dc *sqliteRdbDialectCreator) Create(_ *Bag) (gorm.Dialector, error) {
	host := dc.config.Host
	if len(dc.config.Params) > 0 {
		host += "?"
		for key, value := range dc.config.Params {
			host += fmt.Sprintf("&%s=%v", key, value)
		}
	}
	return sqlite.Open(host), nil
}

type mysqlRdbDialectCreator struct {
	config struct {
		Dialect  string
		Username string
		Password string
		Protocol string
		Host     string
		Port     int
		Schema   string
		Params   Bag
	}
}

var _ RdbDialectCreator = &mysqlRdbDialectCreator{}

func (dc *mysqlRdbDialectCreator) Accept(config *Bag) bool {
	dc.config.Protocol = "tcp"
	dc.config.Host = "localhost"
	dc.config.Port = 3306
	if e := config.Populate("", &dc.config); e != nil {
		return false
	}
	return strings.ToLower(dc.config.Dialect) == "mysql" &&
		dc.config.Username != "" &&
		dc.config.Password != "" &&
		dc.config.Protocol != "" &&
		dc.config.Host != "" &&
		dc.config.Schema != ""
}

func (dc *mysqlRdbDialectCreator) Create(_ *Bag) (gorm.Dialector, error) {
	host := fmt.Sprintf(
		"%s:%s@%s(%s:%d)/%s",
		dc.config.Username,
		dc.config.Password,
		dc.config.Protocol,
		dc.config.Host,
		dc.config.Port,
		dc.config.Schema,
	)
	if len(dc.config.Params) > 0 {
		host += "?"
		for key, value := range dc.config.Params {
			host += fmt.Sprintf("&%s=%v", key, value)
		}
	}
	return mysql.Open(host), nil
}

type postgresRdbDialectCreator struct {
	config struct {
		Dialect  string
		Username string
		Password string
		Host     string
		Port     int
		Schema   string
		Params   Bag
	}
}

var _ RdbDialectCreator = &postgresRdbDialectCreator{}

func (dc *postgresRdbDialectCreator) Accept(config *Bag) bool {
	dc.config.Host = "localhost"
	dc.config.Port = 3306
	if e := config.Populate("", &dc.config); e != nil {
		return false
	}
	return strings.ToLower(dc.config.Dialect) == "postgres" &&
		dc.config.Username != "" &&
		dc.config.Password != "" &&
		dc.config.Host != "" &&
		dc.config.Schema != ""
}

func (dc *postgresRdbDialectCreator) Create(_ *Bag) (gorm.Dialector, error) {
	host := fmt.Sprintf(
		"user=%s password=%s host=%s port=%d dbname=%s",
		dc.config.Username,
		dc.config.Password,
		dc.config.Host,
		dc.config.Port,
		dc.config.Schema,
	)
	if len(dc.config.Params) > 0 {
		host += " "
		for key, value := range dc.config.Params {
			host += fmt.Sprintf(" %s=%v", key, value)
		}
	}
	return postgres.Open(host), nil
}

type rdbConnFactory struct {
	dialectFactory *rdbDialectFactory
}

func newRdbConnFactory(dialectFactory *rdbDialectFactory) *rdbConnFactory {
	return &rdbConnFactory{
		dialectFactory: dialectFactory,
	}
}

func (f *rdbConnFactory) create(config *Bag, gormConfig *gorm.Config) (*gorm.DB, error) {
	dialect, e := f.dialectFactory.Create(config)
	if e != nil {
		return nil, e
	}
	conn, e := gorm.Open(dialect, gormConfig)
	if e != nil {
		return nil, e
	}
	return conn, nil
}

// RdbConnPool @todo doc
type RdbConnPool interface {
	io.Closer

	Get(name string, gormConfig *gorm.Config) (*gorm.DB, error)
}

type rdbConnPool struct {
	connFactory *rdbConnFactory
	connections map[string]*gorm.DB
	config      *Bag
}

var _ RdbConnPool = &rdbConnPool{}

func newRdbConnPool(config Config, connFactory *rdbConnFactory) *rdbConnPool {
	return &rdbConnPool{
		connFactory: connFactory,
		connections: map[string]*gorm.DB{},
		config:      config.Bag(RdbConfigPath, &Bag{}),
	}
}

func (p *rdbConnPool) Close() error {
	for _, conn := range p.connections {
		if db, e := conn.DB(); db != nil && e == nil {
			_ = db.Close()
		}
	}
	p.connections = map[string]*gorm.DB{}
	return nil
}

func (p *rdbConnPool) Get(name string, gormConfig *gorm.Config) (*gorm.DB, error) {
	if conn, ok := p.connections[name]; ok {
		return conn, nil
	}
	return p.create(name, gormConfig)
}

func (p *rdbConnPool) create(name string, gormConfig *gorm.Config) (*gorm.DB, error) {
	conn, e := p.connFactory.create(p.config.Bag(fmt.Sprintf("connections.%s", name), &Bag{}), gormConfig)
	if e != nil {
		return nil, e
	}
	p.connections[name] = conn
	return conn, nil
}

type rdbProvider struct {
	app App
}

var _ Provider = &rdbProvider{}

func newRdbProvider() Provider {
	return &rdbProvider{}
}

func (*rdbProvider) ID() string {
	return "flam.rdb"
}

func (p *rdbProvider) Reg(app App) error {
	_ = app.DI().Provide(func() *gorm.Config { return &gorm.Config{Logger: logger.Discard} })
	_ = app.DI().Provide(newRdbDialectFactory)
	_ = app.DI().Provide(newRdbConnFactory)
	_ = app.DI().Provide(func() RdbDialectCreator { return &sqliteRdbDialectCreator{} }, dig.Group(RdbDialectTag))
	_ = app.DI().Provide(func() RdbDialectCreator { return &mysqlRdbDialectCreator{} }, dig.Group(RdbDialectTag))
	_ = app.DI().Provide(func() RdbDialectCreator { return &postgresRdbDialectCreator{} }, dig.Group(RdbDialectTag))
	_ = app.DI().Provide(newRdbConnPool)
	_ = app.DI().Provide(func(pool *rdbConnPool) RdbConnPool { return pool })
	_ = app.DI().Provide(func(pool *rdbConnPool, config *gorm.Config) (*gorm.DB, error) {
		return pool.Get(RdbPrimaryConnName, config)
	})
	p.app = app
	return nil
}

func (p *rdbProvider) Close() error {
	return p.app.DI().Invoke(func(pool *rdbConnPool) error {
		return pool.Close()
	})
}
