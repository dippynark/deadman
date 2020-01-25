package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/model"
	"github.com/prometheus/common/promlog"
	promlogflag "github.com/prometheus/common/promlog/flag"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

const (
	pushbulletAccessTokenEnvVarName = "PUSHBULLET_ACCESS_TOKEN"
)

func main() {
	cfg := struct {
		pushbulletAccessToken    string
		pushbulletDeviceNickname string
		interval                 model.Duration
		logLevel                 promlog.AllowedLevel
	}{}

	app := kingpin.New(filepath.Base(os.Args[0]), "A deadman's snitch for Prometheus Alertmanager compatible notifications.")
	app.HelpFlag.Short('h')

	pushbulletAccessToken, set := os.LookupEnv(pushbulletAccessTokenEnvVarName)
	if !set {
		fmt.Fprintln(os.Stderr, fmt.Errorf("Environment variable %s not set", pushbulletAccessTokenEnvVarName))
		os.Exit(2)
	}
	cfg.pushbulletAccessToken = pushbulletAccessToken
	app.Flag("interval", "The heartbeat interval. An alert is sent if no heartbeat is sent.").
		Default("30s").SetValue(&cfg.interval)
	app.Flag("pushbullet-device-nickname", "The nickname for the device you want to receive Pushbullet notifications.").Required().
		StringVar(&cfg.pushbulletDeviceNickname)

	promlogflag.AddFlags(app, &cfg.logLevel)

	_, err := app.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, errors.Wrapf(err, "Error parsing commandline arguments"))
		app.Usage(os.Args[1:])
		os.Exit(2)
	}

	pinger := make(chan time.Time)
	http.Handle("/metrics", promhttp.Handler())
	http.Handle("/", simpleHandler(pinger))
	go http.ListenAndServe(":9095", nil)

	logger := promlog.New(cfg.logLevel)

	d, err := NewDeadMan(pinger, time.Duration(cfg.interval), cfg.pushbulletAccessToken, cfg.pushbulletDeviceNickname, log.With(logger, "component", "deadman"))
	if err != nil {
		level.Error(logger).Log("err", err)
		os.Exit(2)
	}

	d.Run()
}

func simpleHandler(pinger chan<- time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pinger <- time.Now()
		fmt.Fprint(w, "")
	}
}
