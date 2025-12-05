package main

import (
	"flag"
	"os"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var log = logf.Log.WithName("longhorn-external-share-manager")

func main() {
	// Setup logger
	zapOpts := zap.Options{}
	zapOpts.BindFlags(flag.CommandLine)

	// Setup controller options
	o := newOptions()
	o.addFlags(flag.CommandLine)
	flag.Parse()

	logf.SetLogger(zap.New(zap.UseFlagOptions(&zapOpts)))
	mainLog := log.WithName("main")

	mainLog.Info("Starting longhorn-external-share-manager")

	mgr, err := manager.New(config.GetConfigOrDie(), manager.Options{
		HealthProbeBindAddress: o.healthAddr,
		Metrics: metricsserver.Options{
			BindAddress: o.metricsAddr,
		},

		Cache: cache.Options{
			SyncPeriod: &o.syncPeriod,
		},
		LeaderElection:             true,
		LeaderElectionResourceLock: "leases",
		LeaderElectionID:           "longhorn-external-share-manager",
	})
	if err != nil {
		mainLog.Error(err, "Unable to set up longhorn-share-manager")
		os.Exit(1)
	}

	if err := mgr.AddReadyzCheck("ping", healthz.Ping); err != nil {
		mainLog.Error(err, "Unable to create ready check")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("ping", healthz.Ping); err != nil {
		mainLog.Error(err, "Unable to create health check")
		os.Exit(1)
	}

	if err := builder.ControllerManagedBy(mgr).
		For(&corev1.Service{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Complete(&reconcileSVC{
			client: mgr.GetClient(),
			log:    log.WithName("reconciler"),
		}); err != nil {
		mainLog.Error(err, "Unable to set up controller")
		os.Exit(1)
	}

	mainLog.Info("Starting manager")
	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		mainLog.Error(err, "Unable to run manager")
		os.Exit(1)
	}
}
