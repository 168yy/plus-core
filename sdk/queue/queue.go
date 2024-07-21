package queue

import (
	"context"
	"github.com/168yy/plus-core/core/v2/message"
	queueLib "github.com/168yy/plus-core/core/v2/queue"
)

// NewQueue 创建对应上下文队列
func NewQueue(prefix string, queue queueLib.IQueue) queueLib.IQueue {
	return &Queue{
		prefix: prefix,
		queue:  queue,
	}
}

type Queue struct {
	prefix string
	queue  queueLib.IQueue
}

func (e *Queue) String() string {
	return e.queue.String()
}

// Consumer 注册消费者
func (e *Queue) Consumer(ctx context.Context, name string, f queueLib.ConsumerFunc, optionFuncs ...func(*queueLib.ConsumeOptions)) {
	e.queue.Consumer(ctx, name, f, optionFuncs...)
}

// Publish 数据生产者
func (e *Queue) Publish(ctx context.Context, msg message.IMessage, optionFuncs ...func(*queueLib.PublishOptions)) error {
	return e.queue.Publish(ctx, msg, optionFuncs...)
}

// Run 运行
func (e *Queue) Run(ctx context.Context) {
	e.queue.Run(ctx)
}

// Shutdown 停止
func (e *Queue) Shutdown(ctx context.Context) {
	if e.queue != nil {
		e.queue.Shutdown(ctx)
	}
}
