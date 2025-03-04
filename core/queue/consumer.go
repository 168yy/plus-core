package queue

import (
	"context"
	"github.com/168yy/plus-core/core/v2/message"
)

type ConsumerFunc func(ctx context.Context, msg message.IMessage) error

// ConsumeOptions are used to describe how a new consumer will be created.
type ConsumeOptions struct {
	// rabbitmq
	BindingRoutingKeys []string
	BindingExchange    *BindingExchangeOptions
	Concurrency        int
	QOSPrefetch        int
	ConsumerName       string
	ConsumerAutoAck    bool
	// rocketmq
	GroupName         string
	MaxReconsumeTimes int32
	AutoCommit        bool
}
