package queue

import (
	"context"
	"github.com/168yy/plus-core/core/v2/message"
)

type IQueue interface {
	String() string
	Publish(ctx context.Context, message message.IMessage, optionFuncs ...func(*PublishOptions)) error
	Consumer(ctx context.Context, name string, f ConsumerFunc, optionFuncs ...func(*ConsumeOptions))
	Run(ctx context.Context)
	Shutdown(ctx context.Context)
}
