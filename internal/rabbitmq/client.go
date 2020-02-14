// Package rabbitmq implements the triton-core/amqp module
package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/streadway/amqp"

	log "github.com/sirupsen/logrus"
	// don't reimplement the wheel
	"github.com/cenkalti/backoff/v4"
)

// Client is a RabbitMQ client
type Client struct {
	ctx context.Context

	// connection is the active rabbitmq connection
	connection *amqp.Connection

	// number of consumer queues to listen on
	numConsumerQueues int

	// lastPublishRK contains the last routing index that was used
	// to publish to <string> queue
	lastPublishRk map[string]int

	// rk modification mutex
	rkmutex sync.Mutex

	// prefetch
	prefetch int64
}

var (
	// ErrorEnsureExchange is returned when exchanges are unable to be created
	ErrorEnsureExchange = errors.New("failed to ensure exchange")

	// ErrorEnsureConsumerQueues is returned when consumer queues are unable to be created
	ErrorEnsureConsumerQueues = errors.New("failed to ensure consumer queues")
)

// NewClient returns a new rabbitmq client
func NewClient(ctx context.Context, endpoint string) (*Client, error) {
	var conn *amqp.Connection
	// TODO(jaredallard): maybe give up at a certain point?
	err := backoff.Retry(func() error {
		var err error

		fqendpoint := fmt.Sprintf("amqp://%s:%s@%s", os.Getenv("RABBITMQ_USERNAME"), os.Getenv("RABBITMQ_PASSWORD"), endpoint)
		conn, err = amqp.Dial(fqendpoint)
		if err != nil {
			log.Errorf("failed to dial rabbitmq: %v", err)
			return err
		}
		return nil
	}, backoff.NewExponentialBackOff())
	if err != nil {
		return nil, fmt.Errorf("failed to connect to rabbitmq after substantial retries")
	}

	return &Client{
		ctx:               ctx,
		lastPublishRk:     make(map[string]int),
		connection:        conn,
		prefetch:          10,
		numConsumerQueues: 2,
	}, nil
}

// ensureExchange ensures that your exchanges exists. Uses a separate channel
// to prevent explosions
func (c *Client) ensureExchange(topic string) error {
	aChan, err := c.getChannel()
	if err != nil {
		return err
	}
	defer aChan.Close()

	return aChan.ExchangeDeclare(topic, "direct", true, false, false, false, amqp.Table{})
}

// ensureConsumerQueues ensures that consumer queues we expect to exist do
func (c *Client) ensureConsumerQueues(topic string) error {
	aChan, err := c.getChannel()
	if err != nil {
		return err
	}
	defer aChan.Close()

	for i := 0; i != c.numConsumerQueues; i++ {
		queue := c.getRk(topic, i)

		if _, err := aChan.QueueDeclare(queue, true, false, false, false, amqp.Table{}); err != nil {
			return err
		}

		if err := aChan.QueueBind(queue, queue, topic, false, amqp.Table{}); err != nil {
			return err
		}
	}

	return nil
}

// getChannel creates a new channel
func (c *Client) getChannel() (*amqp.Channel, error) {
	channel, err := c.connection.Channel()
	if channel != nil {
		if err := channel.Qos(int(c.prefetch), 0, true); err != nil {
			return nil, err
		}
	}
	return channel, err
}

// Channel returns a raw RabbitMQ channel
func (c *Client) Channel() (*amqp.Channel, error) {
	return c.getChannel()
}

// getRK gets the expected queue and rk name for a numberic consumer
func (c *Client) getRk(topic string, rkIndex int) string {
	return fmt.Sprintf("%s-%d", topic, rkIndex)
}

// SetPrefetch updates the prefetch of our channels
func (c *Client) SetPrefetch(prefetch int64) {
	c.prefetch = prefetch
}

// Publish a message to an exchange, must be a serialized format
func (c *Client) Publish(topic string, body []byte) error {
	// TODO(jaredallard): consolidate to using one active channel?
	aChan, err := c.getChannel()
	if err != nil {
		return err
	}
	defer aChan.Close()

	if err := c.ensureExchange(topic); err != nil {
		return ErrorEnsureExchange
	}
	if err := c.ensureConsumerQueues(topic); err != nil {
		return ErrorEnsureConsumerQueues
	}

	rkIndex := c.lastPublishRk[topic]
	rk := c.getRk(topic, rkIndex)

	c.rkmutex.Lock()
	c.lastPublishRk[topic]++
	if c.lastPublishRk[topic] == c.numConsumerQueues {
		c.lastPublishRk[topic] = 0
	}
	c.rkmutex.Unlock()

	// TODO(jaredallard): queue messages in memory
	if err := aChan.Publish(topic, rk, false, false, amqp.Publishing{
		DeliveryMode: amqp.Persistent,
		ContentType:  "application/octet-stream",
		Body:         body,
	}); err == amqp.ErrClosed {
		return c.Publish(topic, body)
	}
	return err
}

// Consume from a RabbitMQ queue
func (c *Client) Consume(topic string) (<-chan *Delivery, <-chan error, error) {
	// TODO(jaredallard): handle channel disconnect
	aChan, err := c.getChannel()
	if err != nil {
		return nil, nil, err
	}

	if err := c.ensureExchange(topic); err != nil {
		return nil, nil, ErrorEnsureExchange
	}
	if err := c.ensureConsumerQueues(topic); err != nil {
		return nil, nil, ErrorEnsureConsumerQueues
	}

	var wg sync.WaitGroup

	multiplexer := make(chan *Delivery)
	errChan := make(chan error)

	wg.Add(c.numConsumerQueues)
	for i := 0; i != c.numConsumerQueues; i++ {
		queue := c.getRk(topic, i)
		ch, err := aChan.Consume(queue, "", false, false, false, false, nil)
		if err != nil {
			return nil, nil, err
		}

		// pipe from this consumer into multiplexed channel
		go func() {
			defer func() {
				errChan <- fmt.Errorf("queue processor '%s' closed", queue)
				wg.Done()
			}()

			for {
				select {
				case msg, ok := <-ch:
					// channel has closed, we do nothing with this message, if we even have one
					if !ok {
						// TODO(jaredallard): when we lose all channels, i.e conn failure, this will trigger.
						// find a better way to handle this
						return
					}

					d, err := NewDelivery(msg, aChan)
					if err != nil {
						errChan <- err
						continue
					}

					// publish the message onto our "combined" queue
					multiplexer <- d

				case <-c.ctx.Done():
					return
				}
			}
		}()
	}

	// wait for our processor threads to finish
	go func() {
		wg.Wait()
		errChan <- fmt.Errorf("all threads closed")

		// all the threads have closed, close our channels now
		close(errChan)
		close(multiplexer)
	}()

	return multiplexer, errChan, nil
}
