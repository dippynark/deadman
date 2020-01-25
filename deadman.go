package main

import (
	"fmt"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"

	"github.com/hashicorp/go-multierror"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/xconstruct/go-pushbullet"
)

const (
	tickPeriod = 2 * 60 * 12 // Number of 30s intervals in 12 hours
)

var (
	ticksTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "deadman_ticks_total",
			Help: "The total ticks passed in this snitch",
		},
	)

	ticksNotified = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "deadman_ticks_notified",
			Help: "The number of ticks where notifications were sent.",
		},
	)

	failedNotifications = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "deadman_notifications_failed",
			Help: "The number of failed notifications.",
		},
	)
)

func init() {
	prometheus.MustRegister(
		ticksTotal,
		ticksNotified,
		failedNotifications,
	)
}

func NewDeadMan(pinger <-chan time.Time, interval time.Duration, pushbulletAccessToken, pushbulletDeviceNickname string, logger log.Logger) (*Deadman, error) {
	return newDeadMan(pinger, interval, pushbulletNotifier(pushbulletAccessToken, pushbulletDeviceNickname), logger), nil
}

type Deadman struct {
	pinger   <-chan time.Time
	interval time.Duration
	ticker   *time.Ticker
	closer   chan struct{}

	notifier func(bool) error

	logger log.Logger
}

func newDeadMan(pinger <-chan time.Time, interval time.Duration, notifier func(bool) error, logger log.Logger) *Deadman {
	return &Deadman{
		pinger:   pinger,
		interval: interval,
		notifier: notifier,
		logger:   logger,
		closer:   make(chan struct{}),
	}
}

func (d *Deadman) Run() error {
	d.ticker = time.NewTicker(d.interval)

	skip := false
	firing := false
	tickCounter := 0

	for {
		select {
		case <-d.ticker.C:
			ticksTotal.Inc()

			if !skip {
				if tickCounter == 0 {
					ticksNotified.Inc()
					if err := d.notifier(true); err != nil {
						failedNotifications.Inc()
						level.Error(d.logger).Log("err", err)
						tickCounter--
					} else {
						firing = true
					}
				}
				tickCounter++
				if tickCounter == tickPeriod {
					tickCounter = 0
				}
			}
			skip = false

		case <-d.pinger:
			skip = true
			tickCounter = 0
			if firing {
				ticksNotified.Inc()
				if err := d.notifier(false); err != nil {
					failedNotifications.Inc()
					level.Error(d.logger).Log("err", err)
				} else {
					firing = false
				}
			}

		case <-d.closer:
			break
		}
	}

	return nil
}

func (d *Deadman) Stop() {
	if d.ticker != nil {
		d.ticker.Stop()
	}

	d.closer <- struct{}{}
}

func pushbulletNotifier(pushbulletAccessToken, pushbulletDeviceNickname string) func(bool) error {

	return func(alert bool) error {

		message := "Receiving Alertmanager alerts again"
		if alert {
			message = "Stopped receiving Alertmanager alerts"
		}

		// create pushbullet client
		pb := pushbullet.New(pushbulletAccessToken)

		devices, err := pb.Devices()
		if err != nil {
			return err
		}

		var merr error

		messageSent := false
		for _, device := range devices {
			if device.Nickname == pushbulletDeviceNickname {
				// push note
				err = pb.PushNote(device.Iden, "Deadman's Snitch", message)
				if err != nil {
					merr = multierror.Append(merr, err)
					continue
				}
				messageSent = true
			}
		}

		if !messageSent {
			merr = fmt.Errorf("failed to send Pushbullet message to device with nickname %s", pushbulletDeviceNickname)
		}

		return merr
	}
}
