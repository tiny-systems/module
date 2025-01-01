package cli

import (
	"fmt"
	"github.com/go-logr/zerologr"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/pkg/utils"
	"golang.org/x/net/context"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

var preDeleteCmd = &cobra.Command{
	Use:   "pre-delete",
	Short: "pre-delete module",
	Run: func(cmd *cobra.Command, args []string) {

		l := zerologr.New(&log.Logger)
		l.Info("starting pre-delete hook")
		if name == "" {
			l.Error(ErrInvalidModuleName, "module name is empty")
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
		defer cancel()

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

		kubeClient, err := client.New(config, client.Options{Scheme: scheme})
		if err != nil {
			l.Error(err, "unable to create kubeclient")
			return
		}

		module := &v1alpha1.TinyModule{
			ObjectMeta: metav1.ObjectMeta{
				Name:      utils.SanitizeResourceName(name),
				Namespace: namespace,
			},
		}

		err = kubeClient.Delete(ctx, module)
		if err != nil && !errors.IsNotFound(err) {
			l.Error(err, "unable to delete tinymodule")
			return
		}
		l.Info("tinymodule deleted", "name", module.Name, "namespace", namespace)
	},
}
