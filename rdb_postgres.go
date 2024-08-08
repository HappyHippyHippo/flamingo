//go:build postgres

package flam

import (
	"fmt"
	"strings"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func init() {
	rdbDialectCreators = append(rdbDialectCreators, func() RdbDialectCreator { return &postgresRdbDialectCreator{} })
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
