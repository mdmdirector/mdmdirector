package db

import (
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/mdmdirector/mdmdirector/utils"
	"github.com/pkg/errors"

	// Need to import postgres
	"gorm.io/driver/postgres"
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

	var newLogger logger.Interface
	if utils.DebugMode() {
		newLogger = logger.New(
			log.New(os.Stdout, "\r\n", log.LstdFlags), // io writer
			logger.Config{
				SlowThreshold: time.Second, // Slow SQL threshold
				LogLevel:      logger.Info, // Log level
				Colorful:      true,        // Disable color
			},
		)
	} else {
		newLogger = logger.New(
			log.New(os.Stdout, "\r\n", log.LstdFlags), // io writer
			logger.Config{
				SlowThreshold: time.Second,   // Slow SQL threshold
				LogLevel:      logger.Silent, // Log level
				Colorful:      false,         // Disable color
			},
		)
	}

	var err error
	DB, err = gorm.Open(postgres.Open(dbURI), &gorm.Config{Logger: newLogger, DisableForeignKeyConstraintWhenMigrating: true})
	if err != nil {
		return errors.Wrap(err, "Open DB")
	}

	err = DB.Exec("CREATE EXTENSION IF NOT EXISTS \"uuid-ossp\";").Error
	if err != nil {
		return errors.Wrap(err, "creating uuid-ossp extension")
	}

	sqlDB, err := DB.DB()
	if err != nil {
		return errors.Wrap(err, "creating sqldb object")
	}

	// SetMaxIdleConns sets the maximum number of connections in the idle connection pool.
	// sqlDB.SetMaxIdleConns(10)

	// SetMaxOpenConns sets the maximum number of open connections to the database.
	sqlDB.SetMaxOpenConns(utils.DBMaxConnections())

	// SetConnMaxLifetime sets the maximum amount of time a connection may be reused.
	sqlDB.SetConnMaxLifetime(time.Hour)

	return nil
}
