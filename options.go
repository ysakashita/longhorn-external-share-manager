package main

import (
	"flag"
	"time"
)

type options struct {
	// controller-manager
	healthAddr  string
	metricsAddr string
	syncPeriod  time.Duration
}

func newOptions() *options {
	return &options{
		healthAddr:  ":9440",
		metricsAddr: ":8080",
		syncPeriod:  1 * time.Minute,
	}
}

func (o *options) addFlags(fs *flag.FlagSet) {
	fs.StringVar(&o.healthAddr, "health-addr", o.healthAddr, "The address the health endpoint binds to")
	fs.StringVar(&o.metricsAddr, "metrics-addr", o.metricsAddr, "The address the metrics endpoint binds to")
	fs.DurationVar(&o.syncPeriod, "sync-period", o.syncPeriod, "The minimum frequency at which watched resources are reconciled")
}
