package cli

import (
	"context"
	"fmt"
	"github.com/go-logr/zerologr"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/internal/controller"
	"github.com/tiny-systems/module/internal/manager"
	"github.com/tiny-systems/module/internal/scheduler"
	"github.com/tiny-systems/module/internal/server"
	m "github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/registry"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sync"
)

// override by ldflags
var (
	namespace            = "tinysystems"
	kubeconfig           string
	version              = "0.1.1"
	name                 string
	versionID            string // ldflags
	metricsAddr          string
	enableLeaderElection bool
	probeAddr            string
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "run module",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {

		// re-use zerolog
		l := zerologr.New(&log.Logger)

		scheme := runtime.NewScheme()

		// register standard and custom schemas
		_ = clientgoscheme.AddToScheme(scheme)
		_ = v1alpha1.AddToScheme(scheme)

		ctrl.SetLogger(l)

		// prepare kubeconfig
		config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			// use InClusterConfig
			config, err = rest.InClusterConfig()
			if err != nil {
				l.Error(err, "unable to get kubeconfig")
				return
			}
		}
		if config == nil {
			l.Error(fmt.Errorf("config is nil"), "unable to create kubeconfig")
			return
		}

		sch := scheduler.New(l)

		// create gRPC server
		serv := server.New(l, sch)

		// create kubebuilder manager
		mgr, err := ctrl.NewManager(config, ctrl.Options{
			Scheme:             scheme,
			Logger:             l,
			MetricsBindAddress: metricsAddr,
			WebhookServer: webhook.NewServer(webhook.Options{
				Port: 9443,
			}),
			HealthProbeBindAddress: probeAddr,
			LeaderElection:         enableLeaderElection,
			LeaderElectionID:       "41928e89.tinysystems.io",
		})
		if err != nil {
			l.Error(err, "unable to create manager")
			return
		}

		controller := &controller.TinyNodeReconciler{
			Client:    mgr.GetClient(),
			Scheme:    mgr.GetScheme(),
			Scheduler: sch,
			Recorder:  mgr.GetEventRecorderFor("tiny-controller"),
		}

		if err = controller.SetupWithManager(mgr); err != nil {
			l.Error(err, "unable to create controller")
			return
		}

		// @todo replace with custom health check
		if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
			l.Error(err, "unable to set up health check")
			return
		}
		// @todo replace with custom health check
		if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
			l.Error(err, "unable to set up health check")
			return
		}

		l.Info("Starting...", "versionID", versionID)

		// run all modules
		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()

		moduleInfo := m.Info{
			Version:   version,
			Name:      name,
			VersionID: versionID,
		}

		crManager := manager.NewManager(mgr.GetClient(), namespace, moduleInfo)

		if err = crManager.UninstallPrevious(ctx, moduleInfo); err != nil {
			l.Error(err, "unable to cleanup previous resources")
			return
		}

		wg := &sync.WaitGroup{}

		l.Info("Installing components")
		for _, cmp := range registry.Get() {
			if err = crManager.Register(ctx, cmp); err != nil {
				l.Error(err, "unable to register", "component", cmp.GetInfo().Name)
			}
			if err := sch.Install(cmp); err != nil {
				l.Error(err, "unable to install", "component", cmp.GetInfo().Name)
			}
		}

		wg.Add(1)
		go func() {
			l.Info("Starting resource manager")
			defer wg.Done()
			defer func() {
				l.Info("Resource manager stopped")
			}()
			if err := crManager.Start(ctx); err != nil {
				l.Error(err, "Problem running resource manager")
			}
		}()

		wg.Add(1)
		go func() {
			l.Info("Starting kubebuilder manager")
			defer wg.Done()
			defer func() {
				l.Info("Kubebuilder manager stopped")
			}()
			if err := mgr.Start(ctx); err != nil {
				l.Error(err, "Problem running kubebuilder")
			}
		}()

		wg.Add(1)
		go func() {
			l.Info("Starting gRPC server")
			defer wg.Done()
			defer func() {
				l.Info("gRPC server stopped")
			}()
			if err := serv.Start(ctx); err != nil {
				l.Error(err, "Problem starting gRPC server")
			}
		}()

		wg.Add(1)
		go func() {
			l.Info("Starting scheduler")
			defer wg.Done()
			defer func() {
				l.Info("Scheduler stopped")
			}()
			if err := sch.Start(ctx); err != nil {
				l.Error(err, "Unable to start scheduler")
			}
		}()

		l.Info("Waiting...")
		wg.Wait()

		l.Info("All done")
	},
}
