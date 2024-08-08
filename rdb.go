package flam

import (
	"fmt"
	"io"

	"go.uber.org/dig"
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

	// RdbConnConfigPath @todo doc
	RdbConnConfigPath = EnvString("FLAMINGO_RDB_CONN_CONFIG_PATH", "flam.rdb.connections")
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
	config      Config
	connFactory *rdbConnFactory
	connections map[string]*gorm.DB
}

var _ RdbConnPool = &rdbConnPool{}

func newRdbConnPool(config Config, connFactory *rdbConnFactory) *rdbConnPool {
	return &rdbConnPool{
		config:      config,
		connFactory: connFactory,
		connections: map[string]*gorm.DB{},
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
	path := fmt.Sprintf("%s.%s", RdbConnConfigPath, name)
	config := p.config.Bag(path, &Bag{})
	conn, e := p.connFactory.create(config, gormConfig)
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
	_ = app.DI().Provide(newRdbConnPool)
	_ = app.DI().Provide(func(pool *rdbConnPool) RdbConnPool { return pool })
	_ = app.DI().Provide(func(pool *rdbConnPool, config *gorm.Config) (*gorm.DB, error) {
		return pool.Get(RdbPrimaryConnName, config)
	})
	for _, dialectCreator := range rdbDialectCreators {
		_ = app.DI().Provide(dialectCreator, dig.Group(RdbDialectTag))
	}
	p.app = app
	return nil
}

func (p *rdbProvider) Close() error {
	return p.app.DI().Invoke(func(pool *rdbConnPool) error {
		return pool.Close()
	})
}
