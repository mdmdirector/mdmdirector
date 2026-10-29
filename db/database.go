package db

import (
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/mdmdirector/mdmdirector/director/metrics"
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
	// pgx sends unrecognised DSN keys to the server as session parameters, so every
	// pooled connection starts with this statement_timeout. Without it a query that
	// stalls (a missing index on a large table, lock contention) holds its connection
	// indefinitely, and under load the whole pool fills with them while callers queue
	// with no error. With it the stalled statements fail and are counted as errors.
	if timeout := utils.DBStatementTimeout(); timeout > 0 {
		dbURI += fmt.Sprintf(" statement_timeout=%d", timeout*1000)
	}

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
	if utils.DBMaxIdleConnections() != -1 {
		sqlDB.SetMaxIdleConns(utils.DBMaxIdleConnections())
	}

	// SetMaxOpenConns sets the maximum number of open connections to the database.
	sqlDB.SetMaxOpenConns(utils.DBMaxConnections())

	// SetConnMaxLifetime sets the maximum amount of time a connection may be reused.
	sqlDB.SetConnMaxLifetime(time.Duration(utils.DBConnMaxLifetime()) * time.Second)

	// SetConnMaxIdleTime closes idle connections before an in-mesh proxy (Istio
	// sidecar / NLB) resets them out from under the pool. Without this, the pool
	// hands a silently-dead connection to the next query, which fails with
	// "connection reset by peer" / "unexpected EOF".
	sqlDB.SetConnMaxIdleTime(time.Duration(utils.DBConnMaxIdleTime()) * time.Second)

	if utils.Prometheus() {
		if err := metrics.RegisterDBStats(sqlDB, utils.DBName()); err != nil {
			return errors.Wrap(err, "registering db pool metrics")
		}

		if err := registerMetricsCallbacks(DB); err != nil {
			return errors.Wrap(err, "registering db metrics callbacks")
		}
	}

	return nil
}

// Migrate runs AutoMigrate on one connection with statement_timeout disabled, so a
// migration that builds an index on a large table is not cancelled part way through
// by -db-statement-timeout. The connection's timeout is restored before it goes back
// to the pool.
// migrationLockKey identifies the PostgreSQL advisory lock that serialises startup
// migrations across instances. Any constant works as long as every instance agrees on it.
const migrationLockKey int64 = 0x6d646d6469726563 // "mdmdirec"

// Migrate runs AutoMigrate while holding a session advisory lock, so when several
// instances start at once only one of them changes the schema. The others block on the
// lock and only run AutoMigrate (a no-op by then) after the first has finished. Callers
// do not serve traffic or start workers until Migrate returns, so no instance writes
// against a half-migrated schema. The lock belongs to the connection, so if the
// migrating instance dies PostgreSQL releases it and the next waiter takes over.
func Migrate(dst ...interface{}) error {
	return DB.Connection(func(tx *gorm.DB) error {
		// Waiting on the lock and building indexes can both take longer than any
		// statement_timeout set for normal traffic.
		if err := tx.Exec("SET statement_timeout = 0").Error; err != nil {
			return errors.Wrap(err, "disabling statement_timeout for migrations")
		}

		var acquired bool
		if err := tx.Raw("SELECT pg_try_advisory_lock(?)", migrationLockKey).Scan(&acquired).Error; err != nil {
			return errors.Wrap(err, "acquiring migration lock")
		}
		if !acquired {
			log.Print("Another instance is running DB migrations, waiting for it to finish")
			start := time.Now()
			if err := tx.Exec("SELECT pg_advisory_lock(?)", migrationLockKey).Error; err != nil {
				return errors.Wrap(err, "waiting for migration lock")
			}
			log.Printf("Migration lock acquired after %s", time.Since(start).Round(time.Second))
		}

		migrateErr := tx.AutoMigrate(dst...)

		if err := tx.Exec("SELECT pg_advisory_unlock(?)", migrationLockKey).Error; err != nil && migrateErr == nil {
			return errors.Wrap(err, "releasing migration lock")
		}
		if err := tx.Exec("RESET statement_timeout").Error; err != nil && migrateErr == nil {
			return errors.Wrap(err, "restoring statement_timeout after migrations")
		}
		return migrateErr
	})
}
