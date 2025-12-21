package cli

import (
	"context"
	"errors"
	"fmt"
	"github.com/cenkalti/backoff/v4"
	"github.com/go-logr/zerologr"
	"github.com/goccy/go-json"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/internal/client"
	"github.com/tiny-systems/module/internal/controller"
	sch "github.com/tiny-systems/module/internal/scheduler"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	"github.com/tiny-systems/module/internal/server"
	"github.com/tiny-systems/module/internal/tracker"
	m "github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/metrics"
	"github.com/tiny-systems/module/pkg/resource"
	"github.com/tiny-systems/module/pkg/utils"
	"github.com/tiny-systems/module/registry"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"golang.org/x/sync/errgroup"
	"io"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog/v2"
	"net"
	"os"
	"reflect"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"strings"
	"sync/atomic"
	"time"
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
			metrics.WithBatchSpanProcessorOption(func(o *sdktrace.BatchSpanProcessorOptions) {
				o.BatchTimeout = 500 * time.Millisecond // Very responsive
				// Optional: also reduce batch size for faster sends
				o.MaxExportBatchSize = 10
			}),
		)
		if err != nil {
			l.Error(err, "configure opentelemetry error")
			os.Exit(1)
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

		// Suppress klog errors from client-go leader election
		klog.SetOutput(io.Discard)

		l.Info("starting")

		if name == "" {
			l.Error(ErrInvalidModuleName, "module name is empty")
			os.Exit(1)
			return
		}
		if version == "" {
			l.Error(ErrInvalidModuleVersion, "module name is empty")
			os.Exit(1)
			return
		}

		if strings.HasPrefix(version, "v") {
			l.Error(ErrInvalidModuleVersion, "version should not start with v prefix")
			os.Exit(1)
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
				os.Exit(1)
				return
			}
		}
		if config == nil {
			l.Error(fmt.Errorf("config is nil"), "unable to create kubeconfig")
			os.Exit(1)
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
			os.Exit(1)
			return
		}

		// custom leader elector
		coreClient, err := kubernetes.NewForConfig(config)
		if err != nil {
			l.Error(err, "unable to create core kubernetes client for leader election")
			os.Exit(1)
		}

		podName := os.Getenv("HOSTNAME")
		if podName == "" {
			l.Error(err, "HOSTNAME environment variable not set, required for leader identity")
			os.Exit(1)
		}

		isLeader := &atomic.Bool{}
		isLeader.Store(false)

		lock, err := resourcelock.New(
			resourcelock.LeasesResourceLock,
			namespace,
			fmt.Sprintf("%s-lock", utils.SanitizeResourceName(name)),
			nil,
			coreClient.CoordinationV1(), // Event recorder
			resourcelock.ResourceLockConfig{
				Identity: utils.SanitizeResourceName(podName),
			},
		)
		if err != nil {
			l.Error(err, "unable to create resource lock for custom leader election")
			os.Exit(1)
		}

		leaderElected := make(chan struct{})

		leaderCallbacks := leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				l.Info("became leader for status updates")
				isLeader.Store(true)
			},
			OnStoppedLeading: func() {
				l.Info("stopped leading for status updates")
				isLeader.Store(false)
			},
			OnNewLeader: func(identity string) {
				l.Info("new leader elected for status updates", "leader", identity)
				if isClosed(leaderElected) {
					return
				}
				close(leaderElected)
			},
		}

		elector, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
			Lock:            lock,
			LeaseDuration:   15 * time.Second,
			RenewDeadline:   10 * time.Second,
			RetryPeriod:     2 * time.Second,
			Callbacks:       leaderCallbacks,
			ReleaseOnCancel: true,
			Name:            fmt.Sprintf("%s-leader-elector", utils.SanitizeResourceName(name)),
		})
		if err != nil {
			l.Error(err, "unable to create leader elector for status updates")
			os.Exit(1)
		}

		cmdCtx, cancel := context.WithCancelCause(cmd.Context())
		defer cancel(nil)

		wg, ctx := errgroup.WithContext(cmdCtx)

		// Start the custom leader elector in a goroutine

		wg.Go(func() error {

			leLogger := l.WithName("custom-leader-elector-loop")

			backoffDuration := 5 * time.Second    // Initial backoff duration
			maxBackoffDuration := 5 * time.Minute // Maximum backoff duration

			for {
				leLogger.Info("starting custom leader election process")

				elector.Run(ctx)
				// If we reach here, elector.Run() has exited.

				select {
				case <-ctx.Done():
					leLogger.Info("context cancelled, will not retry leader election", "reason", context.Cause(ctx))

					if err := context.Cause(ctx); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
						return err
					}
					return nil
				default:
					// The context is not done, so elector.Run() likely exited due to losing the lease
					// or an unrecoverable error during renewal (like the API being unreachable).
					leLogger.Error(nil, "custom leader election ended unexpectedly, will retry after backoff", "current_backoff", backoffDuration)

					// Wait for the backoff period, but also listen for context cancellation.
					select {
					case <-time.After(backoffDuration):
						// Exponential backoff
						backoffDuration *= 2
						if backoffDuration > maxBackoffDuration {
							backoffDuration = maxBackoffDuration
						}
					case <-ctx.Done():
						leLogger.Info("context cancelled during backoff, stopping leader election retries", "reason", context.Cause(ctx))
						return nil
					}
				}
			}

		})

		listenAddr := make(chan string)
		defer close(listenAddr)

		var (
			tracer = otel.Tracer(name)
			meter  = otel.Meter(name)
			pool   = client.NewPool().SetLogger(l)
		)

		//
		resourceManager, err := resource.NewManagerFromConfig(config, namespace)
		if err != nil {
			l.Error(err, "unable to create resource manager")
			os.Exit(1)
		}

		var (
			trackManager = tracker.NewManager().SetLogger(l)
			scheduler    *sch.Schedule
		)

		//
		scheduler = sch.New(func(ctx context.Context, msg *runner.Msg) (any, error) {
			m, _, err := m.ParseFullName(msg.To)
			if err != nil {
				return nil, fmt.Errorf("parse destination error: %v", err)
			}

			if m == moduleInfo.GetNameSanitised() {
				// destination is the current module
				return scheduler.Handle(ctx, msg)
			}

			// gRPC call with retries
			var resp []byte

			err = backoff.Retry(func() error {
				resp, err = pool.Handler(ctx, msg)
				return err

			}, backoff.WithContext(backoff.NewExponentialBackOff(func(off *backoff.ExponentialBackOff) {

				off.Multiplier = 1.1               // do not slow down fast
				off.MaxElapsedTime = 0             // never give up
				off.MaxInterval = 30 * time.Second // max interval not too long

			}), ctx))
			if err != nil {
				return nil, err
			}

			if msg.Resp == nil {
				return resp, nil
			}

			respData := reflect.New(reflect.TypeOf(msg.Resp)).Elem()
			if err = json.Unmarshal(resp, respData.Addr().Interface()); err != nil {
				return nil, err
			}
			return respData.Interface(), err
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
			IsLeader:  isLeader,
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
			IsLeader:   isLeader,
		}

		if err = moduleController.SetupWithManager(mgr); err != nil {
			l.Error(err, "unable to create tinymodule controller")
			return
		}

		if err = (&controller.TinyTrackerReconciler{
			Client:   mgr.GetClient(),
			Scheme:   mgr.GetScheme(),
			Manager:  trackManager,
			IsLeader: isLeader,
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
			IsLeader:  isLeader,
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

		if err = resourceManager.CreateModule(ctx, moduleInfo); err != nil {
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

		wg.Go(func() error {
			<-leaderElected

			// start manager
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

		// start grpc client pool

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

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
	}
	return false
}
