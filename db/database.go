package db

import (
	"fmt"

	"github.com/jinzhu/gorm"

	"github.com/mdmdirector/mdmdirector/utils"

	// Need to import postgres
	_ "github.com/jinzhu/gorm/dialects/postgres"
)

var DB *gorm.DB

func Open() error {

	username := utils.DBUsername()
	password := utils.DBPassword()
	dbName := utils.DBName()
	dbHost := utils.DBHost()

	dbUri := fmt.Sprintf("host=%s user=%s dbname=%s sslmode=disable password=%s", dbHost, username, dbName, password)
	var err error
	DB, err = gorm.Open("postgres", dbUri)
	if err != nil {
		return err
	}

	return nil
}

func Close() error {
	return DB.Close()
}
