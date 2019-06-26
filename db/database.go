package db

import (
	"github.com/grahamgilbert/mdmdirector/settings"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
)

var DB *gorm.DB

func Open() error {
	var err error
	settingsDict := settings.LoadSettings()
	DB, err = gorm.Open(settingsDict.DatabaseType, settingsDict.ConnectionString)
	if err != nil {
		return err
	}

	return nil

}

func Close() error {
	return DB.Close()
}
