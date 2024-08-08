//go:build mysql

package flam

import (
	"fmt"
	"strings"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func init() {
	rdbDialectCreators = append(rdbDialectCreators, func() RdbDialectCreator { return &mysqlRdbDialectCreator{} })
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
