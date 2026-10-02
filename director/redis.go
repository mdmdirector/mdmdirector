package director

import (
	"crypto/tls"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/mdmdirector/mdmdirector/utils"
)

func RedisClient() *redis.Client {

	host := utils.RedisHost()
	port := utils.RedisPort()
	password := utils.RedisPassword()

	connectionString := fmt.Sprintf("%v:%v", host, port)

	opts := &redis.Options{
		Addr:     connectionString,
		Password: password,
		DB:       0,
	}

	if utils.RedisTLS() {
		opts.TLSConfig = &tls.Config{}
	}

	rdb := redis.NewClient(opts)
	time.Sleep(5 * time.Second)
	return rdb
}
