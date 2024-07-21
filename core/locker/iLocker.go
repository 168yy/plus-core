package locker

import "github.com/168yy/redislock"

type ILocker interface {
	String() string
	Lock(key string, ttl int64, options ...redislock.Option) (*redislock.Mutex, error)
}
