// Короткие таймауты и ноль повторов: кеш не должен тормозить запрос, когда
// Redis недоступен, поэтому любая ошибка быстро превращается в промах и запрос
// идёт в базу.

package cache

import (
	"time"

	"github.com/redis/go-redis/v9"
)

const operationTimeout = 200 * time.Millisecond

func NewClient(addr string) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:         addr,
		DialTimeout:  operationTimeout,
		ReadTimeout:  operationTimeout,
		WriteTimeout: operationTimeout,
		PoolTimeout:  operationTimeout,
		MaxRetries:   -1,
	})
}
