package cli

import (
	"context"
	"fmt"
	"github.com/go-logr/zerologr"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/internal/client"
	"github.com/tiny-systems/module/internal/controller"
	"github.com/tiny-systems/module/internal/manager"
	"github.com/tiny-systems/module/internal/scheduler"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	"github.com/tiny-systems/module/internal/server"
	m "github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/registry"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"net"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"strings"
)

// override by ldflags
var (
	namespace            = "tinysystems"
	kubeconfig           string
	version              = "0.1.7"
	name                 string
	versionID            string // ldflags
	metricsAddr          string
	grpcAddr             string
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

		// module name (sanitised) to its grpc server address
		moduleAddrLookup := cmap.New[string]()

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
			LeaderElectionID:       fmt.Sprintf("%s.tinysystems.io", name),
		})
		if err != nil {
			l.Error(err, "unable to create manager")
			return
		}

		// run all modules
		ctx, cancel := context.WithCancelCause(cmd.Context())
		defer cancel(nil)

		wg, ctx := errgroup.WithContext(ctx)

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
				l.Error(err, "Problem starting gRPC server")
				return err
			}
			return nil
		})

		// add listening address to the module info
		moduleInfo.Addr = <-listenAddr

		//
		sch := scheduler.New().SetLogger(l)

		nodeController := &controller.TinyNodeReconciler{
			Client:    mgr.GetClient(),
			Scheme:    mgr.GetScheme(),
			Scheduler: sch,
			Recorder:  mgr.GetEventRecorderFor("tiny-controller"),
			Module:    moduleInfo,
		}

		if err = nodeController.SetupWithManager(mgr); err != nil {
			l.Error(err, "unable to create node controller")
			return
		}

		moduleController := &controller.TinyModuleReconciler{
			Client:       mgr.GetClient(),
			Scheme:       mgr.GetScheme(),
			Module:       moduleInfo,
			AddressTable: moduleAddrLookup,
		}

		if err = moduleController.SetupWithManager(mgr); err != nil {
			l.Error(err, "unable to create module controller")
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

		l.Info("Installing components", "versionID", versionID)

		crManager := manager.NewManager(mgr.GetClient(), namespace, moduleInfo)

		// uninstall palette resources from previous versions

		if err = crManager.Cleanup(ctx); err != nil {
			l.Error(err, "unable to cleanup previous versions components")
			return
		}

		l.Info("Registering", "module", moduleInfo.GetMajorName())

		if err = crManager.RegisterModule(ctx); err != nil {
			l.Error(err, "unable to register a module")
			return
		}
		///

		for _, cmp := range registry.Get() {

			l.Info("Registering", "component", cmp.GetInfo().Name)

			if err = crManager.RegisterComponent(ctx, cmp); err != nil {
				l.Error(err, "unable to register", "component", cmp.GetInfo().Name)
			}
			if err := sch.Install(cmp); err != nil {
				l.Error(err, "unable to install", "component", cmp.GetInfo().Name)
			}
		}

		wg.Go(func() error {
			if err := crManager.Start(ctx); err != nil {
				l.Error(err, "Problem starting manager lookup")
				return err
			}
			return nil
		})

		// kubebuilder start

		wg.Go(func() error {
			l.Info("Starting kubebuilder manager")
			defer func() {
				l.Info("Kubebuilder manager stopped")
			}()
			if err := mgr.Start(ctx); err != nil {
				l.Error(err, "Problem running kubebuilder")
				return err
			}
			return nil
		})

		pool := client.NewPool(moduleAddrLookup).SetLogger(l)
		outputCh := make(chan *runner.Msg)
		defer close(outputCh)

		// grpc client start

		wg.Go(func() error {
			l.Info("Starting client pool")
			defer func() {
				l.Info("Client pool stopped")
			}()
			if err := pool.Start(ctx, outputCh); err != nil {
				l.Error(err, "Client pool error")
				return err
			}
			return nil
		})

		/// Instance scheduler loop

		wg.Go(func() error {
			l.Info("Starting scheduler")
			defer func() {
				l.Info("Scheduler stopped")
			}()
			if err := sch.Start(ctx, inputCh, outputCh); err != nil {
				l.Error(err, "Unable to start scheduler")
				return err
			}
			return nil
		})

		////

		l.Info("Waiting...")
		wg.Wait()

		l.Info("All done")
	},
}
