package rabbitmq

import (
	"context"
	"encoding/json"
	messageLib "github.com/168yy/plus-core/core/v2/message"
	queueLib "github.com/168yy/plus-core/core/v2/queue"
	"github.com/168yy/plus-core/sdk/v2/message"
	"github.com/gogf/gf/v2/os/gctx"
	"github.com/gogf/gf/v2/os/glog"
	"sync"
	"time"
)

func NewRabbitMQ(
	ctx context.Context,
	dsn string,
	reconnectInterval int,
	cfg *rabbitmq.Config,
	logger rabbitmq.Logger,
) (*RabbitMQ, error) {
	//var err error
	//var consumer rabbitmq.Consumer
	r := &RabbitMQ{
		Url:               dsn,
		ReconnectInterval: reconnectInterval,
		producers:         map[string]*rabbitmq.Publisher{},
		consumers:         map[string]*rabbitmq.Consumer{},
		Logger:            logger,
	}
	if cfg != nil {
		r.Config = *cfg
	}
	return r, nil
}

// RabbitMQ cache implement
type RabbitMQ struct {
	Url               string
	ReconnectInterval int
	Handler           []rabbitmq.Handler
	Config            rabbitmq.Config
	mux               sync.RWMutex
	consumers         map[string]*rabbitmq.Consumer
	ConsumerOptions   *rabbitmq.ConsumerOptions
	producers         map[string]*rabbitmq.Publisher
	PublisherOptions  *rabbitmq.PublisherOptions
	Logger            rabbitmq.Logger
	conn              *rabbitmq.Conn
}

func (r *RabbitMQ) String() string {
	return "rabbitmq"
}

func (r *RabbitMQ) newConn(ctx context.Context) (*rabbitmq.Conn, error) {
	var err error
	if r.conn == nil {
		r.conn, err = rabbitmq.NewConn(
			ctx,
			r.Url,
			rabbitmq.WithConnectionOptionsLogger(r.Logger),
			rabbitmq.WithConnectionOptionsConfig(r.Config),
			rabbitmq.WithConnectionOptionsReconnectInterval(time.Duration(r.ReconnectInterval)*time.Second),
		)
		if err != nil {
			return nil, err
		}
	}
	return r.conn, nil
}

func (r *RabbitMQ) newConsumer(ctx context.Context, queueName string, handler rabbitmq.Handler, options queueLib.ConsumeOptions) (*rabbitmq.Consumer, error) {
	var err error
	var conn *rabbitmq.Conn
	conn, err = r.newConn(ctx)
	if err != nil {
		return nil, err
	}
	return rabbitmq.NewConsumer(ctx,
		conn,
		handler,
		queueName,
		rabbitmq.WithConsumerOptionsExchangeDeclare,
		rabbitmq.WithConsumerOptionsLogger(r.Logger),
		rabbitmq.WithConsumerOptionsRoutingKeys(options.BindingRoutingKeys),
		rabbitmq.WithConsumerOptionsConsumerName(options.ConsumerName),
		rabbitmq.WithConsumerOptionsExchangeName(options.BindingExchange.Name),
		rabbitmq.WithConsumerOptionsExchangeKind(options.BindingExchange.Kind),
		rabbitmq.WithConsumerOptionsConcurrency(options.Concurrency), // goroutine num
		rabbitmq.WithConsumerOptionsConsumerAutoAck(options.ConsumerAutoAck),
		rabbitmq.WithConsumerOptionsQOSPrefetch(options.QOSPrefetch),
		rabbitmq.WithConsumerOptionsExchangeDurable,
		rabbitmq.WithConsumerOptionsQueueDurable,
	)
}

func (r *RabbitMQ) newProducer(ctx context.Context) (*rabbitmq.Publisher, error) {
	var err error
	var conn *rabbitmq.Conn
	conn, err = r.newConn(ctx)
	if err != nil {
		return nil, err
	}

	return rabbitmq.NewPublisher(ctx,
		conn,
		rabbitmq.WithPublisherOptionsLogger(r.Logger),
	)
}

// Publish 消息入生产者
func (r *RabbitMQ) Publish(ctx context.Context, message messageLib.IMessage, optionFuncs ...func(*queueLib.PublishOptions)) error {
	// exchange exchangeType routingKey
	rb, err := json.Marshal(message.GetValues())
	if err != nil {
		return err
	}
	options := &queueLib.PublishOptions{
		ContentType: "application/json",
		MessageID:   gctx.CtxId(ctx),
	}
	for _, optionFunc := range optionFuncs {
		optionFunc(options)
	}
	var p *rabbitmq.Publisher
	var ok bool
	if p, ok = r.producers[options.Exchange]; !ok {
		p, err = r.newProducer(ctx)
		if err != nil {
			glog.Warning(ctx, "rabbitmq newProducer error:", err)
			return err
		}
		r.producers[options.Exchange] = p
	}

	err = p.PublishWithContext(
		ctx,
		rb,
		[]string{message.GetRoutingKey()},
		rabbitmq.WithPublishOptionsExchange(options.Exchange),
		rabbitmq.WithPublishOptionsContentType(options.ContentType),
		rabbitmq.WithPublishOptionsMessageID(options.MessageID),
		rabbitmq.WithPublishOptionsAppID(options.AppID),
		rabbitmq.WithPublishOptionsUserID(options.UserID),
		rabbitmq.WithPublishOptionsReplyTo(options.ReplyTo),
		rabbitmq.WithPublishOptionsMandatory,
		rabbitmq.WithPublishOptionsPersistentDelivery,
	)
	return err
}

// Consumer 监听消费者
func (r *RabbitMQ) Consumer(ctx context.Context, queueName string, f queueLib.ConsumerFunc, optionFuncs ...func(*queueLib.ConsumeOptions)) {
	options := queueLib.GetDefaultConsumeOptions()
	for _, optionFunc := range optionFuncs {
		optionFunc(&options)
	}
	var c *rabbitmq.Consumer
	var err error
	var ok bool
	r.mux.Lock()
	defer r.mux.Unlock()
	if c, ok = r.consumers[options.BindingExchange.Name]; !ok {
		header := func(d rabbitmq.Delivery) rabbitmq.Action {
			m := new(message.Message)
			m.SetValues(map[string]interface{}{
				"body": string(d.Body),
			})
			m.SetRoutingKey(d.RoutingKey)
			m.SetId(d.MessageId)
			if d.Redelivered {
				m.SetErrorCount(d.DeliveryTag)
			}
			err = f(ctx, m)
			if err != nil {
				glog.Warning(ctx, "RabbitMQ Requeue msg:", m)
				return rabbitmq.NackRequeue
			}
			// rabbitmq.Ack, rabbitmq.NackDiscard, rabbitmq.NackRequeue
			return rabbitmq.Ack
		}
		c, err = r.newConsumer(ctx, queueName, header, options)
		if err != nil {
			glog.Error(ctx, "rabbitmq newConsumer error:", err)
			return
		}
		r.consumers[options.BindingExchange.Name] = c
	}

}

func (r *RabbitMQ) Run(ctx context.Context) {
	return
}

func (r *RabbitMQ) Shutdown(ctx context.Context) {
	for _, pd := range r.producers {
		pd.Close(ctx)
	}
	for _, pushConsumer := range r.consumers {
		pushConsumer.Close(ctx)
	}
	if r.conn != nil {
		err := r.conn.Close(ctx)
		if err != nil {
			glog.Warning(ctx, "rabbitmq conn close error:", err.Error())
		}
	}
}
