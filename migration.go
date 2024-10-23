package flam

import (
	"errors"
	"sort"
	"time"

	"go.uber.org/dig"
	"gorm.io/gorm"
)

const (
	// MigrationTag @todo doc
	MigrationTag = "flam.migration"
)

var (
	// MigrationDatabase @todo doc
	MigrationDatabase = EnvString("FLAMINGO_MIGRATION_DATABASE", RdbPrimaryConnName)

	// AutoMigrate @todo doc
	AutoMigrate = EnvBool("FLAMINGO_MIGRATION_AUTO_MIGRATE", true)
)

type migrationRecord struct {
	ID uint `json:"id" xml:"id" gorm:"primaryKey"`

	Version uint64 `json:"version" xml:"version"`

	CreatedAt time.Time  `json:"createdAt" xml:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt" xml:"updatedAt"`
	DeletedAt *time.Time `json:"deletedAt" xml:"deletedAt" sql:"index"`
}

func (migrationRecord) TableName() string {
	return "__migrations"
}

type migrationDAO struct {
	migrated bool
	conn     *gorm.DB
}

func newMigrationDao(connPool RdbConnPool, config *gorm.Config) (*migrationDAO, error) {
	conn, e := connPool.Get(MigrationDatabase, config)
	if e != nil {
		return nil, e
	}
	return &migrationDAO{
		conn: conn,
	}, nil
}

func (d *migrationDAO) last() (*migrationRecord, error) {
	if e := d.migrate(); e != nil {
		return nil, e
	}
	model := &migrationRecord{}
	result := d.conn.Order("created_at desc").FirstOrInit(model, migrationRecord{Version: 0})
	if result.Error != nil {
		if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, result.Error
		}
	}
	return model, nil
}

func (d *migrationDAO) up(version uint64) (*migrationRecord, error) {
	if e := d.migrate(); e != nil {
		return nil, e
	}
	model := &migrationRecord{Version: version}
	if result := d.conn.Create(model); result.Error != nil {
		return nil, result.Error
	}
	return model, nil
}

func (d *migrationDAO) down(last *migrationRecord) error {
	if e := d.migrate(); e != nil {
		return e
	}
	if last.Version != 0 {
		if result := d.conn.Unscoped().Delete(last); result.Error != nil {
			return result.Error
		}
	}
	return nil
}

func (d *migrationDAO) migrate() error {
	if !d.migrated {
		if e := d.conn.AutoMigrate(&migrationRecord{}); e != nil {
			return e
		}
		d.migrated = true
	}
	return nil
}

// Migration @todo doc
type Migration interface {
	Version() uint64
	Up() error
	Down() error
}

// Migrator @todo doc
type Migrator interface {
	List() []uint64
	Current() (uint64, error)
	Up() error
	Down() error
}

type migrator struct {
	dao        *migrationDAO
	migrations []Migration `group:"flam.migration"`
}

var _ Migrator = &migrator{}

func newMigrator(
	args struct {
		dig.In
		Dao        *migrationDAO
		Migrations []Migration `group:"flam.migration"`
	},
) (Migrator, error) {
	sort.Slice(args.Migrations, func(i, j int) bool {
		return args.Migrations[i].Version() < args.Migrations[j].Version()
	})
	return &migrator{
		dao:        args.Dao,
		migrations: args.Migrations,
	}, nil
}

func (m *migrator) AddMigration(migration Migration) {
	m.migrations = append(m.migrations, migration)
	sort.Slice(m.migrations, func(i, j int) bool {
		return m.migrations[i].Version() < m.migrations[j].Version()
	})
}

func (m *migrator) List() []uint64 {
	var list []uint64
	for _, migration := range m.migrations {
		list = append(list, migration.Version())
	}
	return list
}

func (m *migrator) Current() (uint64, error) {
	current, e := m.dao.last()
	if e != nil {
		return 0, e
	}
	return current.Version, nil
}

func (m *migrator) Migrate() error {
	if len(m.migrations) == 0 {
		return nil
	}
	current, e := m.dao.last()
	if e != nil {
		return e
	}
	for _, migration := range m.migrations {
		if v := migration.Version(); current.Version < v {
			if e := migration.Up(); e != nil {
				return e
			}
			if current, e = m.dao.up(v); e != nil {
				return e
			}
		}
	}
	return nil
}

func (m *migrator) Up() error {
	if len(m.migrations) == 0 {
		return nil
	}
	current, e := m.dao.last()
	if e != nil {
		return e
	}
	for _, migration := range m.migrations {
		if v := migration.Version(); current.Version < v {
			if e := migration.Up(); e != nil {
				return e
			}
			_, e = m.dao.up(v)
			return e
		}
	}
	return nil
}

func (m *migrator) Down() error {
	if len(m.migrations) == 0 {
		return nil
	}
	current, e := m.dao.last()
	if e != nil {
		return e
	}
	for _, migration := range m.migrations {
		if v := migration.Version(); current.Version == v {
			if e := migration.Down(); e != nil {
				return e
			}
			return m.dao.down(current)
		}
	}
	return nil
}

type migrationInitializer struct {
	migrator *migrator
}

func newMigrationInitializer(m *migrator) *migrationInitializer {
	return &migrationInitializer{
		migrator: m,
	}
}

func (i *migrationInitializer) init() error {
	if !AutoMigrate {
		return nil
	}
	return i.migrator.Migrate()
}

func (i *migrationInitializer) close() error {
	return nil
}

type migrationProvider struct {
	app App
}

var _ Provider = &migrationProvider{}

func newMigrationProvider() *migrationProvider {
	return &migrationProvider{}
}

func (*migrationProvider) ID() string {
	return "flam.migration"
}

func (p *migrationProvider) Reg(app App) error {
	_ = app.DI().Provide(newMigrationDao)
	_ = app.DI().Provide(newMigrator)
	_ = app.DI().Provide(func(m *migrator) Migrator { return m })
	_ = app.DI().Provide(newMigrationInitializer)
	p.app = app
	return nil
}

func (p *migrationProvider) Boot() error {
	return p.app.DI().Invoke(func(i *migrationInitializer) error {
		return i.init()
	})
}

func (p *migrationProvider) Close() error {
	return p.app.DI().Invoke(func(i *migrationInitializer) error {
		return i.close()
	})
}
