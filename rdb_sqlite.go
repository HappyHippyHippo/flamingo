//go:build sqlite

package flam

import (
	"fmt"
	"strings"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func init() {
	rdbDialectCreators = append(rdbDialectCreators, func() RdbDialectCreator { return &sqliteRdbDialectCreator{} })
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
