package main

import (
	"encoding/json"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"time"

	log "github.com/Sirupsen/logrus"
	firehose "github.com/seatgeek/nomad-firehose/command/allocations"
	sink "github.com/seatgeek/nomad-firehose/sink"
	"github.com/streadway/amqp"
)

var (
	maxRestarts          int
	restartInterval      time.Duration
	notificationInterval time.Duration
)

// Ledger ...
type Ledger struct {
	items         map[string][]time.Time
	notifications map[string]time.Time
	event         map[string]firehose.AllocationUpdate
	sync.Mutex
}

// Event ...
type Event struct {
	LastEvent firehose.AllocationUpdate
	EventLog  []time.Time
}

func main() {
	var err error

	maxRestarts, err = strconv.Atoi(os.Getenv("RESTART_COUNT"))
	if err != nil {
		log.Fatalf("Invalid RESTART_COUNT - must be an integer")
	}

	restartInterval, err = time.ParseDuration(os.Getenv("RESTART_INTERVAL"))
	if err != nil {
		log.Fatal("Invalid or missing RESTART_INTERVAL")
	}

	notificationInterval, err = time.ParseDuration(os.Getenv("NOTIFICATION_INTERVAL"))
	if err != nil {
		log.Fatal("Invalid or missing NOTIFICATION_INTERVAL")
	}

	connStr := os.Getenv("AMQP_CONNECTION")
	if connStr == "" {
		log.Fatalf("Missing AMQP_CONNECTION (example: amqp://guest:guest@127.0.0.1:5672/)")
	}

	queue := os.Getenv("AMQP_QUEUE")
	if queue == "" {
		log.Fatalf("Missing AMQP_QUEUE")
	}

	conn, err := amqp.Dial(connStr)
	if err != nil {
		log.Fatalf("Failed to connect to AMQP: %s", err)
	}

	defer conn.Close()

	stopCh := make(chan interface{})

	go signalHandler(stopCh)
	rescive(conn, queue, stopCh)
}

func rescive(conn *amqp.Connection, queue string, stopCh chan interface{}) {
	ch, err := conn.Channel()
	if err != nil {
		return
	}
	defer ch.Close()

	sink, err := sink.GetSink()
	if err != nil {
		log.Fatal(err)
	}
	go sink.Start()

	msgs, err := ch.Consume(
		queue, // queue
		"",    // consumer
		true,  // auto-ack
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,   // args
	)

	ledger := &Ledger{
		items:         make(map[string][]time.Time),
		notifications: make(map[string]time.Time),
		event:         make(map[string]firehose.AllocationUpdate),
	}

	processTicker := time.NewTicker(5 * time.Second)

	for {
		select {
		case msg := <-msgs:
			// Read and unmarshal the allocation from RabbitMQ
			var alloc firehose.AllocationUpdate
			if err := json.Unmarshal(msg.Body, &alloc); err != nil {
				panic(err)
			}

			// We only care about restarting events
			if alloc.TaskEvent.Type != "Restarting" {
				continue
			}

			// Prevent concurrent writes to the hashmap
			ledger.Lock()

			// Ensure the ledger contain the alloc name
			if _, ok := ledger.items[alloc.Name]; !ok {
				ledger.items[alloc.Name] = []time.Time{}
			}

			// Convert nanoseconds to a time.Date object
			tm := time.Unix(0, alloc.TaskEvent.Time)

			// Append the restart time to the allocation ledger
			log.Infof("Saw restart on process %s @ %s", alloc.Name, tm)
			ledger.items[alloc.Name] = append(ledger.items[alloc.Name], tm)
			ledger.event[alloc.Name] = alloc

			ledger.Unlock()

		case <-processTicker.C:
			// While processing the ledger, we lock the ledger
			// causing the rabbitmq ingress to pause so we avoid
			// concurrent writes
			ledger.Lock()
			process(ledger, sink)
			ledger.Unlock()

		case <-stopCh:
			return
		}
	}
}

func process(ledger *Ledger, sink sink.Sink) {
	now := time.Now()

	// Iterate all ledger items
	for alloc := range ledger.items {
		// Remove all event times that are older than our restartInterval
		ledger.items[alloc] = choose(ledger.items[alloc], func(eventTime time.Time) bool {
			return now.Sub(eventTime) < restartInterval
		})

		// Check if the remainig event time list for the alloc is smaller than maxRestarts
		// if that is the case, the process haven't crashed enough to warrent a notification
		numberOfRestarts := len(ledger.items[alloc])
		if numberOfRestarts == 0 {
			log.Infof("No more registered restarts for %s", alloc)
			delete(ledger.notifications, alloc)
			delete(ledger.items, alloc)
			delete(ledger.event, alloc)
			continue
		}

		if numberOfRestarts < maxRestarts {
			// Remove the last notification tracking for the allocation if thats the case
			// as it no longer triggers alerts, meaning it either went away or stopped crash-looping
			if _, ok := ledger.notifications[alloc]; ok {
				log.Infof("Don't track last notification for %s no more", alloc)
				delete(ledger.notifications, alloc)
			}

			continue
		}

		// Check if we have notified on this alloc before within our notificationInterval
		lastTime, ok := ledger.notifications[alloc]
		if ok && now.Sub(lastTime) < notificationInterval {
			// Don't notify again
			log.Infof("[%s] Already notified on crash loop for within the last %s", alloc, notificationInterval)
			continue
		}

		// Notify on the crash-loop, and store when we did it
		ledger.notifications[alloc] = now
		log.Warnf("[%s] Notifying on crashloop: %d times over %s", alloc, numberOfRestarts, restartInterval)

		e := Event{
			LastEvent: ledger.event[alloc],
			EventLog:  ledger.items[alloc],
		}

		b, err := json.Marshal(e)
		if err != nil {
			log.Error(err)
		}

		sink.Put(b)
	}
}

// On signal, gracefully stop the process and go routines
func signalHandler(stopCh chan interface{}) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	select {
	case <-c:
		log.Info("Caught signal, releasing lock and stopping...")
		close(stopCh)
	case <-stopCh:
		break
	}
}

// Convinience function to filter a list of times on a lambda
func choose(ss []time.Time, test func(time.Time) bool) (ret []time.Time) {
	for _, s := range ss {
		if test(s) {
			ret = append(ret, s)
		}
	}

	return ret
}
