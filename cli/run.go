package cli

import (
	"context"
	"fmt"
	"github.com/go-logr/zerologr"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/internal/client"
	"github.com/tiny-systems/module/internal/controller"
	"github.com/tiny-systems/module/internal/manager"
	"github.com/tiny-systems/module/internal/scheduler"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	"github.com/tiny-systems/module/internal/server"
	"github.com/tiny-systems/module/internal/tracker"
	m "github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/registry"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"net"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"strings"
)

// override by ldflags
var (
	namespace            string
	kubeconfig           string
	version              string
	name                 string
	versionID            string // ldflags
	metricsAddr          string
	grpcAddr             string
	enableLeaderElection bool
	probeAddr            string
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run module",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		defer func() {
			if r := recover(); r != nil {
				fmt.Println("recovered", r)
			}
		}()

		// re-use zerolog
		l := zerologr.New(&log.Logger)

		moduleInfo := m.Info{
			Version:   version,
			Name:      name,
			VersionID: versionID,
		}

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

		// create kubebuilder manager
		mgr, err := ctrl.NewManager(config, ctrl.Options{
			Scheme: scheme,
			Logger: l,
			Metrics: metricsserver.Options{
				BindAddress: metricsAddr,
			},
			//WebhookServer: webhook.NewServer(webhook.Options{
			//	Port: 9443,
			//}),
			Cache:                  cache.Options{},
			HealthProbeBindAddress: probeAddr,
			LeaderElection:         enableLeaderElection,
			LeaderElectionID:       fmt.Sprintf("%s.tinysystems.io", name),
		})
		if err != nil {
			l.Error(err, "unable to create manager")
			return
		}

		// run all modules
		cmdCtx, cancel := context.WithCancelCause(cmd.Context())
		defer cancel(nil)

		wg, ctx := errgroup.WithContext(cmdCtx)

		listenAddr := make(chan string)
		defer close(listenAddr)

		// create gRPC server

		serv := server.New().SetLogger(l)
		inputCh := make(chan *runner.Msg)
		defer close(inputCh)

		wg.Go(func() error {
			l.Info("Starting gRPC server")

			defer func() {
				l.Info("gRPC server stopped")
			}()
			if err := serv.Start(ctx, inputCh, grpcAddr, func(addr net.Addr) {
				// @todo check if inside of container
				parts := strings.Split(addr.String(), ":")
				if len(parts) > 0 {
					listenAddr <- fmt.Sprintf("127.0.0.1:%s", parts[len(parts)-1])
					return
				}
				l.Info("gRPC listen address", "addr", addr.String())
				listenAddr <- addr.String()
			}); err != nil {
				l.Error(err, "problem starting gRPC server")
				return err
			}
			return nil
		})

		// add listening address to the module info
		moduleInfo.Addr = <-listenAddr

		crManager := manager.NewManager(mgr.GetClient(), l, namespace)

		//
		sch := scheduler.New().
			SetLogger(l).
			SetManager(crManager)

		nodeController := &controller.TinyNodeReconciler{
			Client:    mgr.GetClient(),
			Scheme:    mgr.GetScheme(),
			Scheduler: sch,
			Recorder:  mgr.GetEventRecorderFor("tiny-controller"),
			Module:    moduleInfo,
		}

		if err = nodeController.SetupWithManager(mgr); err != nil {
			l.Error(err, "unable to create tinynode controller")
			return
		}

		pool := client.NewPool().SetLogger(l)

		moduleController := &controller.TinyModuleReconciler{
			Client:     mgr.GetClient(),
			Scheme:     mgr.GetScheme(),
			Module:     moduleInfo,
			ClientPool: pool,
		}

		if err = moduleController.SetupWithManager(mgr); err != nil {
			l.Error(err, "unable to create tinymodule controller")
			return
		}

		trackManager := tracker.NewManager().
			SetLogger(l)

		if err = (&controller.TinyTrackerReconciler{
			Client:  mgr.GetClient(),
			Scheme:  mgr.GetScheme(),
			Manager: trackManager,
			//
		}).SetupWithManager(mgr); err != nil {
			l.Error(err, "unable to create tinytracker controller")
			return
		}

		if err = (&controller.TinySignalReconciler{
			Client:    mgr.GetClient(),
			Scheme:    mgr.GetScheme(),
			Scheduler: sch,
			Module:    moduleInfo,
		}).SetupWithManager(mgr); err != nil {
			l.Error(err, "unable to create tinysignal controller")
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

		l.Info("installing components", "versionID", versionID)

		// uninstall palette resources from previous versions
		if err = crManager.CleanupExampleNodes(ctx, moduleInfo); err != nil {
			l.Error(err, "unable to cleanup previous versions components")
			return
		}

		l.Info("registering", "module", moduleInfo.GetMajorName())

		if err = crManager.RegisterModule(ctx, moduleInfo); err != nil {
			l.Error(err, "unable to register a module")
			return
		}
		///

		for _, cmp := range registry.Get() {
			l.Info("registering", "component", cmp.GetInfo().Name)

			if err = crManager.RegisterExampleNode(ctx, cmp, moduleInfo); err != nil {
				l.Error(err, "unable to register", "component", cmp.GetInfo().Name)
			}
			if err := sch.Install(cmp); err != nil {
				l.Error(err, "unable to install", "component", cmp.GetInfo().Name)
			}
		}

		wg.Go(func() error {
			l.Info("resource manager started")

			defer func() {
				l.Info("resource manager stopped")
			}()
			if err := crManager.Start(ctx); err != nil {
				l.Error(err, "problem during resource manager start")
				return err
			}
			return nil
		})

		// kubebuilder start

		wg.Go(func() error {
			l.Info("starting kubebuilder operator")
			defer func() {
				l.Info("kubebuilder operator stopped")
			}()
			if err := mgr.Start(ctx); err != nil {
				l.Error(err, "problem during kubebuilder operator start")
				return err
			}
			return nil
		})

		eventBus := make(chan *runner.Msg)
		defer close(eventBus)

		// grpc client start

		wg.Go(func() error {
			l.Info("starting gRPC client pool")
			defer func() {
				l.Info("gRPC client pool stopped")
			}()
			if err := pool.Start(ctx, eventBus); err != nil {
				l.Error(err, "client pool error")
				return err
			}
			return nil
		})
		// tracker
		/// Instance scheduler
		wg.Go(func() error {
			l.Info("starting scheduler")
			defer func() {
				l.Info("scheduler stopped")
			}()
			if err := sch.Start(ctx, wg, inputCh, eventBus, func(msg tracker.PortMsg) {
				go func() {
					// @todo add pool
					trackManager.Track(ctx, msg)
				}()
			}); err != nil {
				l.Error(err, "unable to start scheduler")
				return err
			}
			return nil
		})
		////
		// run tracker

		wg.Go(func() error {
			l.Info("starting tracker manager")
			defer func() {
				l.Info("tracker manager stopped")
			}()
			if err := trackManager.Run(ctx); err != nil {
				l.Error(err, "unable to start tracker manager")
				return err
			}
			return nil
		})

		l.Info("waiting...")
		wg.Wait()

		l.Info("all done")
	},
}
