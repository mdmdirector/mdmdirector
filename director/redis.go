package director

import (
	"crypto/tls"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/mdmdirector/mdmdirector/utils"
)

// RedisClient returns the go-redis client shared by the taskq queue and the
// control-plane lock.
func RedisClient() *redis.Client {
	rdb := redis.NewClient(redisOptions())
	time.Sleep(5 * time.Second)
	return rdb
}

// redisOptions builds the client options from flags. The idle and age limits
// recycle pooled connections before an in-mesh proxy (Istio sidecar / NLB)
// resets them, as SetConnMaxIdleTime and SetConnMaxLifetime do for Postgres.
// go-redis treats 0 as "use the library default" and -1 as "disabled" for the
// idle settings, and 0 as "forever" for MaxConnAge.
func redisOptions() *redis.Options {
	opts := &redis.Options{
		Addr:               fmt.Sprintf("%v:%v", utils.RedisHost(), utils.RedisPort()),
		Password:           utils.RedisPassword(),
		DB:                 0,
		IdleTimeout:        secondsOrDisabled(utils.RedisIdleTimeout()),
		MaxConnAge:         time.Duration(utils.RedisMaxConnAge()) * time.Second,
		IdleCheckFrequency: secondsOrDisabled(utils.RedisIdleCheckFrequency()),
	}

	if utils.RedisTLS() {
		opts.TLSConfig = &tls.Config{}
	}

	return opts
}

// secondsOrDisabled converts a seconds flag to a duration, keeping -1 as the
// go-redis "disabled" sentinel.
func secondsOrDisabled(seconds int) time.Duration {
	if seconds < 0 {
		return -1
	}
	return time.Duration(seconds) * time.Second
}
