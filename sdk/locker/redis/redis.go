package redis

import (
	"github.com/168yy/redislock"
	"github.com/168yy/redislock/redis/gredis"
	glib "github.com/gogf/gf/v2/database/gredis"
	"time"
)

// NewRedis 初始化locker
func NewRedis(c *glib.Redis) *Redis {
	return &Redis{
		client: c,
	}
}

type Redis struct {
	client *glib.Redis
	mutex  *redislock.Lock
}

func (Redis) String() string {
	return "redis"
}

func (r *Redis) Lock(key string, ttl int64, options ...redislock.Option) (*redislock.Mutex, error) {
	if r.mutex == nil {
		r.mutex = redislock.New(gredis.NewPool(r.client))
	}
	options = append(options, redislock.WithExpiry(time.Duration(ttl)*time.Second))
	return r.mutex.NewMutex(key, options...), nil
}
