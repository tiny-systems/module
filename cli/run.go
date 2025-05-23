package cli

import (
	"context"
	"fmt"
	"github.com/cenkalti/backoff/v4"
	"github.com/go-logr/zerologr"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/internal/client"
	"github.com/tiny-systems/module/internal/controller"
	"github.com/tiny-systems/module/internal/resource"
	sch "github.com/tiny-systems/module/internal/scheduler"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	"github.com/tiny-systems/module/internal/server"
	"github.com/tiny-systems/module/internal/tracker"
	m "github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/metrics"
	"github.com/tiny-systems/module/registry"
	"go.opentelemetry.io/otel"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"net"
	"os"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"strings"
)

// override by ldflags
var (
	namespace   string
	kubeconfig  string
	version     string
	name        string
	versionID   string // ldflags
	metricsAddr string
	grpcAddr    string
	probeAddr   string
)

var (
	ErrInvalidModuleName    = fmt.Errorf("invalid module name")
	ErrInvalidModuleVersion = fmt.Errorf("invalid module version")
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run module",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {

		// re-use zerolog
		l := zerologr.New(&log.Logger)
		err := metrics.ConfigureOpenTelemetry(
			metrics.WithDSN(os.Getenv("OTLP_DSN")),
		)
		if err != nil {
			l.Error(err, "configure opentelemetry error")
		}

		// Send buffered spans and free resources.
		defer func() {
			ctx := context.Background()
			if err := metrics.ForceFlush(ctx); err != nil {
				l.Error(err, "force flush metrics")
			}
			if err := metrics.Shutdown(ctx); err != nil {
				l.Error(err, "shutdown metrics")
			}
		}()

		defer func() {
			if r := recover(); r != nil {
				l.Info("recovered", "from", r)
			}
		}()

		l.Info("starting")

		if name == "" {
			l.Error(ErrInvalidModuleName, "module name is empty")
			return
		}
		if version == "" {
			l.Error(ErrInvalidModuleVersion, "module name is empty")
			return
		}

		if strings.HasPrefix(version, "v") {
			l.Error(ErrInvalidModuleVersion, "version should not start with v prefix")
			return
		}

		moduleInfo := m.Info{
			Version:   version,
			Name:      name,
			VersionID: versionID,
		}

		scheme := runtime.NewScheme()

		// register standard and custom schemes
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
			Cache: cache.Options{
				DefaultNamespaces: map[string]cache.Config{
					namespace: {},
				},
			},
			HealthProbeBindAddress: probeAddr,
			LeaderElection:         false,
		})
		if err != nil {
			l.Error(err, "unable to create manager")
			return
		}

		// run all systems
		cmdCtx, cancel := context.WithCancelCause(cmd.Context())
		defer cancel(nil)

		wg, ctx := errgroup.WithContext(cmdCtx)

		listenAddr := make(chan string)
		defer close(listenAddr)

		var (
			tracer = otel.Tracer(name)
			meter  = otel.Meter(name)
			pool   = client.NewPool().SetLogger(l)
		)

		//
		var (
			resourceManager = resource.NewManager(mgr.GetClient(), l, namespace)
			trackManager    = tracker.NewManager().SetLogger(l)
			scheduler       *sch.Schedule
		)

		//
		scheduler = sch.New(func(ctx context.Context, msg *runner.Msg) error {
			m, _, err := m.ParseFullName(msg.To)
			if err != nil {
				return fmt.Errorf("parse destination error: %v", err)
			}

			if m == moduleInfo.GetNameSanitised() {
				// destination is the current module
				return scheduler.Handle(ctx, msg)
			}

			// gRPC call with retries

			return backoff.Retry(func() error {
				return pool.Handler(ctx, msg)
			}, backoff.WithContext(backoff.NewExponentialBackOff(), ctx))

		}).
			SetLogger(l).
			SetMeter(meter).
			SetTracker(trackManager).
			SetTracer(tracer).
			SetManager(resourceManager)

		// create gRPC server
		var (
			serv = server.New().SetLogger(l)
		)

		wg.Go(func() error {
			l.Info("starting gRPC server")

			defer func() {
				l.Info("gRPC server stopped")
			}()
			if err := serv.Start(ctx, scheduler.Handle, grpcAddr, func(addr net.Addr) {
				// @todo check if inside of container
				l.Info("gRPC listens to address", "addr", addr.String())

				//
				if selfHost := os.Getenv("SELF_SERVICE_HOST"); selfHost != "" {
					l.Info("gRPC address", "addr", selfHost)
					listenAddr <- selfHost
					return
				}

				parts := strings.Split(addr.String(), ":")
				if len(parts) > 0 {
					localAddr := fmt.Sprintf("127.0.0.1:%s", parts[len(parts)-1])

					l.Info("gRPC address", "localAddr", localAddr)
					listenAddr <- localAddr
					return
				}
				listenAddr <- addr.String()
			}); err != nil {
				l.Error(err, "problem starting gRPC server")
				return err
			}
			return nil
		})

		// add listening address to the module info
		moduleInfo.Addr = <-listenAddr

		nodeController := &controller.TinyNodeReconciler{
			Client:    mgr.GetClient(),
			Scheme:    mgr.GetScheme(),
			Scheduler: scheduler,
			Module:    moduleInfo,
		}

		if err = nodeController.SetupWithManager(mgr); err != nil {
			l.Error(err, "unable to create tinynode controller")
			return
		}

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
			Scheduler: scheduler,
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

		l.Info("registering", "module", moduleInfo.Name)

		if err = resourceManager.RegisterModule(ctx, moduleInfo); err != nil {
			l.Error(err, "unable to register a module")
			return
		}
		///
		for _, cmp := range registry.Get() {
			l.Info("registering", "component", cmp.GetInfo().Name)

			if err := scheduler.Install(cmp); err != nil {
				l.Error(err, "unable to install", "component", cmp.GetInfo().Name)
			}
		}

		wg.Go(func() error {
			l.Info("resource manager started")

			defer func() {
				l.Info("resource manager stopped")
			}()
			if err := resourceManager.Start(ctx); err != nil {
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

		// grpc client start

		wg.Go(func() error {
			l.Info("starting gRPC client pool")
			defer func() {
				l.Info("gRPC client pool stopped")
			}()
			if err := pool.Start(ctx); err != nil {
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
			if err := scheduler.Start(ctx); err != nil {
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
		_ = wg.Wait()

		l.Info("all done")
	},
}
