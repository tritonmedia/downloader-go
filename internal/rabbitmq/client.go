// Package rabbitmq implements the triton-core/amqp module
package rabbitmq

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

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

	// workerContext is used to keep track of the worker threads and terminate
	// them as needed
	workerContext context.Context

	workerMultiplexer map[string]chan *Delivery

	// workerThreads is the number of desired worker threads
	// map[queueName]numberOfWorkers
	workerThreads map[string]int

	// actualThreads is the number of actual threads
	// map[queueName]numberOfWorkers
	actualThreads map[string]int

	// used to signify to external callers that we're done
	done context.Context

	// rk modification mutex
	rkmutex sync.Mutex

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
	workerContext, cancel := context.WithCancel(ctx)
	ourContext, ourCancel := context.WithCancel(context.Background())
	c := Client{
		ctx:               ctx,
		endpoint:          endpoint,
		lastPublishRk:     make(map[string]int),
		workerThreads:     make(map[string]int),
		workerContext:     workerContext,
		actualThreads:     make(map[string]int),
		done:              ourContext,
		workerMultiplexer: make(map[string]chan *Delivery),
		prefetch:          10,
		numConsumerQueues: 2,
	}

	err := c.createConnection()

	t := time.NewTicker(1 * time.Second)

	// create the scheduler thread and connection handling logic
	go func() {
		for {
			select {
			case <-ctx.Done():
				// ensure we've cancelled the worker context
				cancel()
				for {
					// check if all the threads have cancelled, the above context being cancelled
					// would've triggered the worker context being cancelled
					if c.numberOfActualThreads() != 0 {
						log.Infof("waiting on %d workers ...", c.numberOfActualThreads())
						// TODO(jaredallard): we could probably make this a bit better by using channels across the workers
						time.Sleep(1 * time.Second)
						continue
					}

					ourCancel()
					return
				}
			case <-t.C:
				// handle creating the worker threads
				for queueName, num := range c.workerThreads {
					// if we don't have the desired capacity, create new ones
					if num != c.actualThreads[queueName] {
						log.Infof("creating thread '%v'", queueName)
						if err := c.createProcessor(queueName); err != nil {
							log.Errorf("failed to create thread: %v", err)

							// it'll be recreated
							continue
						}

						c.actualThreads[queueName]++
					}
				}

				// if connection is still alive, then we skip this
				if !c.connection.IsClosed() {
					continue
				}

				// we try to recreate the connection
				err := c.createConnection()
				if err == nil {
					// if we had no error creating a connection, then we should cancel all the worker
					// threads and recreate them
					cancel()

					// recreate the worker context, since the old one is finished now
					c.workerContext, cancel = context.WithCancel(ctx)
				}
			}
		}
	}()

	return &c, err
}

func (c *Client) createProcessor(queueName string) error {
	rmqChan, err := c.getChannel()
	if err != nil {
		return err
	}

	ch, err := rmqChan.Consume(queueName, "", false, false, false, false, nil)
	if err != nil {
		return err
	}

	// pipe from this consumer into multiplexed channel
	go func() {
		defer rmqChan.Close()

		for {
			select {
			case msg := <-ch:
				// skip invalid messages, i.e bad data
				if msg.Body == nil {
					continue
				}

				d, err := NewDelivery(c.ctx, msg, rmqChan)
				if err != nil {
					continue
				}

				// publish the message onto our "combined" queue
				c.workerMultiplexer[queueName] <- d

			case <-c.workerContext.Done():
				log.Infof("worker on queue '%s' shutting down", queueName)
				c.actualThreads[queueName]--
				return
			}
		}
	}()

	return nil
}

func (c *Client) numberOfTotalThreads() int {
	total := 0
	for _, i := range c.workerThreads {
		total += i
	}

	return total
}

func (c *Client) numberOfActualThreads() int {
	total := 0
	for _, i := range c.actualThreads {
		total += i
	}

	return total
}

func (c *Client) createConnection() error {
	// TODO(jaredallard): maybe give up at a certain point?
	err := backoff.Retry(func() error {
		var err error

		fqendpoint := fmt.Sprintf("amqp://%s:%s@%s", os.Getenv("RABBITMQ_USERNAME"), os.Getenv("RABBITMQ_PASSWORD"), c.endpoint)
		c.connection, err = amqp.Dial(fqendpoint)
		if err != nil {
			log.Errorf("failed to dial rabbitmq: %v", err)
			return err
		}

		return nil
	}, backoff.NewExponentialBackOff())
	if err != nil {
		return err
	}

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
	if err := c.createConnection(); err != nil {
		return nil, err
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

// Done waits until this client completely closes
func (c *Client) Done() {
	<-c.done.Done()
}

// Consume from a RabbitMQ queue
func (c *Client) Consume(topic string) (msgs <-chan *Delivery, errChan <-chan error, err error) {
	if err := c.ensureExchange(topic); err != nil {
		return nil, nil, ErrorEnsureExchange
	}
	if err := c.ensureConsumerQueues(topic); err != nil {
		return nil, nil, ErrorEnsureConsumerQueues
	}

	multiplexer := make(chan *Delivery)

	for i := 0; i != c.numConsumerQueues; i++ {
		queue := c.getRk(topic, i)
		c.workerThreads[queue]++
		c.workerMultiplexer[queue] = multiplexer
	}

	return multiplexer, make(chan error), nil
}
