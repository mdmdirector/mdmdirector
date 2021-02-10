package director

import (
	"fmt"

	"github.com/go-redis/redis/v8"
	"github.com/mdmdirector/mdmdirector/utils"
)

func RedisClient() *redis.Client {

	host := utils.RedisHost()
	port := utils.RedisPort()
	password := utils.RedisPassword()

	connectionString := fmt.Sprintf("%v:%v", host, port)

	rdb := redis.NewClient(&redis.Options{
		Addr:     connectionString,
		Password: password,
		DB:       0, // use default DB
	})

	return rdb
}
