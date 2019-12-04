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
	dbPort := utils.DBPort()
	dbSSLMode := utils.DBSSLMode()

	dbURI := fmt.Sprintf("host=%s port=%s user=%s dbname=%s sslmode=%s password=%s", dbHost, dbPort, username, dbName, dbSSLMode, password)

	var err error
	DB, err = gorm.Open("postgres", dbURI)
	if err != nil {
		return err
	}

	return nil
}

func Close() error {
	return DB.Close()
}
