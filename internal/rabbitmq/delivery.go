package rabbitmq

import (
	"context"
	"reflect"
	"time"

	"github.com/streadway/amqp"
)

// DeliveryMetadata is metadata from AMQP headers
type DeliveryMetadata struct {
	Retries int
}

// Delivery is an AMQP delivery
type Delivery struct {
	Metadata DeliveryMetadata

	// Delivery is the internal amqp delivery struct
	Delivery amqp.Delivery

	// Channel this message was recieved on
	Channel *amqp.Channel

	// Context is the context this delivery is running under
	Context context.Context
}

// NewDelivery creates a delivery object
func NewDelivery(ctx context.Context, delivery amqp.Delivery, channel *amqp.Channel) (*Delivery, error) {
	retryValue, ok := delivery.Headers["X-Retries"]
	if !ok {
		retryValue = int32(0)
	}

	// handle invalid input
	if reflect.TypeOf(retryValue).String() != "int32" {
		retryValue = int32(0)
	}

	retries := retryValue.(int32)

	return &Delivery{
		Delivery: delivery,
		Metadata: DeliveryMetadata{
			Retries: int(retries),
		},
		Context: ctx,
		Channel: channel,
	}, nil
}

// TODO(jaredallard): add support for queueing ack/nack/etc events in case
// the rabbitmq instance has died

// Ack acks the message
func (d *Delivery) Ack() error {
	return d.Delivery.Ack(false)
}

// Nack dequeues the message
func (d *Delivery) Nack() error {
	return d.Delivery.Nack(false, false)
}

// Error reports an error with a message and reschedules it for a retry
// TODO(jaredallard): use deadletter queues
func (d *Delivery) Error() error {

	// increment the retry counter
	d.Metadata.Retries++

	// HACK: get around no deadletters right now
	time.Sleep(10 * time.Second)

	if err := d.Ack(); err != nil {
		return err
	}

	return d.Channel.Publish(d.Delivery.Exchange, d.Delivery.RoutingKey, false, false, amqp.Publishing{
		Headers: amqp.Table{
			"X-Retries": d.Metadata.Retries,
		},
		Body: d.Delivery.Body,
	})
}
