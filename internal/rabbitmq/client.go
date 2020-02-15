// Package rabbitmq implements the triton-core/amqp module
package rabbitmq

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/pkg/errors"
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

	// connection mutex for tracking the state of a connection
	connmutex sync.Mutex

	// endpoint of the rabbitmq instance
	endpoint string

	// prefetch
	prefetch int64
}

var (
	// ErrorEnsureExchange is returned when exchanges are unable to be created
	ErrorEnsureExchange = errors.New("failed to ensure exchange")

	// ErrorEnsureConsumerQueues is returned when consumer queues are unable to be created
	ErrorEnsureConsumerQueues = errors.New("failed to ensure consumer queues")

	// ErrorReconnecting is emitted when the channel is reconnecting
	ErrorReconnecting = errors.New("processor is reconnecting")

	// ErrorDied is emitted when a processor dies, with no hope of recovering
	ErrorDied = errors.New("processor died")
)

// NewClient returns a new rabbitmq client
func NewClient(ctx context.Context, endpoint string) (*Client, error) {
	c := Client{
		ctx:               ctx,
		endpoint:          endpoint,
		lastPublishRk:     make(map[string]int),
		prefetch:          10,
		numConsumerQueues: 2,
	}

	err := c.createConnection()

	return &c, err
}

func (c *Client) createConnection() error {
	// lock the mutex before we check the state, if we have a new valid connection
	// we just no-op
	c.connmutex.Lock()
	if c.connection != nil {
		if c.connection.IsClosed() {
			c.connection.Close()
		} else {
			// refuse to modify a valid connection, instead it'll just use the valid one
			return nil
		}
	}

	// TODO(jaredallard): maybe give up at a certain point?
	_ = backoff.Retry(func() error {
		var err error

		fqendpoint := fmt.Sprintf("amqp://%s:%s@%s", os.Getenv("RABBITMQ_USERNAME"), os.Getenv("RABBITMQ_PASSWORD"), c.endpoint)
		c.connection, err = amqp.Dial(fqendpoint)
		if err != nil {
			log.Errorf("failed to dial rabbitmq: %v", err)
			return err
		}

		return nil
	}, backoff.NewExponentialBackOff())

	c.connmutex.Unlock()
	return nil
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
	// check the state of our connection
	if c.connection.IsClosed() {
		if err := c.createConnection(); err != nil {
			return nil, err
		}
	}

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

// createProcessor creates a job processor on a given queue
func (c *Client) createProcessor(
	queue string,
	multiplexer chan *Delivery, errChan chan error,
	wg *sync.WaitGroup,
) error {
	rmqChan, err := c.getChannel()
	if err != nil {
		return err
	}

	ch, err := rmqChan.Consume(queue, "", false, false, false, false, nil)
	if err != nil {
		return err
	}

	// pipe from this consumer into multiplexed channel
	go func() {
		for {
			select {
			// default, check if the connection has closed, otherwise we would have gotten
			// a message and thus this block wouldn't have run
			default:
				if c.connection.IsClosed() {
					// close the channel, we don't care about it anymore anyways
					rmqChan.Close()

					errChan <- fmt.Errorf("connection died")
					err := c.createProcessor(queue, multiplexer, errChan, wg)
					if err != nil {
						errChan <- ErrorReconnecting
						continue
					}

					errChan <- fmt.Errorf("processor '%s' reconnected", queue)

					// we terminate because the new processor is taking over
					return
				}
			case msg := <-ch:
				d, err := NewDelivery(c.ctx, msg, rmqChan)
				if err != nil {
					errChan <- err
					continue
				}

				// publish the message onto our "combined" queue
				multiplexer <- d

			case <-c.ctx.Done():
				errChan <- ErrorDied
				wg.Done()
				return
			}
		}
	}()

	return nil
}

// Consume from a RabbitMQ queue
func (c *Client) Consume(topic string) (<-chan *Delivery, <-chan error, error) {
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
		if err := c.createProcessor(queue, multiplexer, errChan, &wg); err != nil {
			wg.Done()
			return nil, nil, errors.Wrap(err, "failed to create initial processor")
		}
	}

	// wait for our processor threads to finish
	// TODO(jaredallard): need to add logic to ensure that these are always running
	go func() {
		wg.Wait()
		errChan <- fmt.Errorf("all threads closed")

		// we ignore connection close errors ultimately
		if err := c.connection.Close(); err != nil {
			errChan <- err
		}

		// all the threads have closed, close our channels now
		close(errChan)
		close(multiplexer)
	}()

	return multiplexer, errChan, nil
}
