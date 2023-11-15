package manager

import (
	"context"
	"fmt"
	"github.com/davecgh/go-spew/spew"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/utils"
	v1core "k8s.io/api/core/v1"
	v1ingress "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

type Resource struct {
	client    client.Client
	namespace string
}

type ResourceInterface interface {
	CleanupExampleNodes(ctx context.Context, mod module.Info) error
	RegisterModule(ctx context.Context, mod module.Info) error
	ExposePort(ctx context.Context, port int) (string, error)
	DisclosePort(ctx context.Context, port int) error
	RegisterExampleNode(ctx context.Context, c module.Component, mod module.Info) error
}

func NewManager(c client.Client, ns string) *Resource {
	return &Resource{client: c, namespace: ns}
}

func (m Resource) CleanupExampleNodes(ctx context.Context, mod module.Info) error {
	sel := labels.NewSelector()

	req, err := labels.NewRequirement(v1alpha1.FlowIDLabel, selection.Equals, []string{""})
	if err != nil {
		return err
	}
	sel = sel.Add(*req)

	req, err = labels.NewRequirement(v1alpha1.ModuleNameLabel, selection.Equals, []string{mod.GetMajorNameSanitised()})
	if err != nil {
		return err
	}
	sel = sel.Add(*req)

	req, err = labels.NewRequirement(v1alpha1.ModuleVersionLabel, selection.NotEquals, []string{mod.Version})
	if err != nil {
		return err
	}
	sel = sel.Add(*req)

	return m.client.DeleteAllOf(ctx, &v1alpha1.TinyNode{}, client.InNamespace(m.namespace), client.MatchingLabelsSelector{
		Selector: sel,
	})
}

func (m Resource) RegisterModule(ctx context.Context, mod module.Info) error {

	spec := v1alpha1.TinyModuleSpec{
		Image: mod.GetFullName(),
	}

	node := &v1alpha1.TinyModule{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   m.namespace, // @todo make dynamic
			Name:        mod.GetMajorNameSanitised(),
			Labels:      map[string]string{},
			Annotations: map[string]string{},
		},
		Spec: spec,
	}

	err := m.client.Create(ctx, node)
	if errors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (m Resource) ExposePort(ctx context.Context, port int) (string, error) {
	currentPod := os.Getenv("HOSTNAME")
	if currentPod == "" {
		return "", fmt.Errorf("unable to determine current pod")
	}

	pod := &v1core.Pod{}
	err := m.client.Get(context.Background(), client.ObjectKey{
		Namespace: m.namespace,
		Name:      currentPod,
	}, pod)

	if err != nil {
		return "", fmt.Errorf("unable to find current pod: %v")
	}

	var releaseName string
	for k, v := range pod.ObjectMeta.Labels {
		if k == "app.kubernetes.io/instance" {
			releaseName = v
		}
	}
	if releaseName == "" {
		return "", fmt.Errorf("release name label not found")
	}

	svc, err := m.getReleaseService(ctx, releaseName)
	if err != nil {
		return "", fmt.Errorf("unable to get service: %v", err)
	}

	spew.Dump("service", svc)
	ingress, _ := m.getReleaseIngress(ctx, releaseName)
	spew.Dump("ingress", ingress)

	fmt.Printf("expose pod %d for pod %s \n", port, currentPod)
	return fmt.Sprintf("https://pub-url-%d", port), nil
}

func (m Resource) getReleaseService(ctx context.Context, releaseName string) (*v1core.Service, error) {
	servicesList := &v1core.ServiceList{}
	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{
			"app.kubernetes.io/instance":  releaseName,
			"app.kubernetes.io/name":      "tinysystems-operator",
			"app.kubernetes.io/component": "manager",
		},
	})

	if err != nil {
		return nil, fmt.Errorf("build service selector error: %s", err)
	}

	if err = m.client.List(ctx, servicesList, client.MatchingLabelsSelector{
		Selector: selector,
	}, client.InNamespace(m.namespace)); err != nil {
		return nil, fmt.Errorf("service list error: %v", err)
	}

	if len(servicesList.Items) == 0 {
		return nil, fmt.Errorf("unable to find manager service")
	}

	if len(servicesList.Items) > 1 {
		return nil, fmt.Errorf("service is ambigous")
	}

	return &servicesList.Items[0], nil
}

func (m Resource) getReleaseIngress(ctx context.Context, releaseName string) (*v1ingress.Ingress, error) {
	ingressList := &v1ingress.IngressList{}
	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{
			"app.kubernetes.io/instance": releaseName,
			"app.kubernetes.io/name":     "tinysystems-operator",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("build ingress selector error: %s", err)
	}

	if err = m.client.List(ctx, ingressList, client.MatchingLabelsSelector{
		Selector: selector,
	}, client.InNamespace(m.namespace)); err != nil {
		return nil, fmt.Errorf("service list error: %v", err)
	}

	if len(ingressList.Items) == 0 {
		return nil, fmt.Errorf("unable to find manager ingress")
	}

	if len(ingressList.Items) > 1 {
		return nil, fmt.Errorf("ingress is ambigous")
	}
	return &ingressList.Items[0], nil
}

func (m Resource) DisclosePort(ctx context.Context, port int) error {
	fmt.Printf("disclose port %d for pod %s\n", port, os.Getenv("HOSTNAME"))
	return nil
}

func (m Resource) RegisterExampleNode(ctx context.Context, c module.Component, mod module.Info) error {

	componentInfo := c.GetInfo()
	node := &v1alpha1.TinyNode{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: m.namespace, // @todo make dynamic
			Name:      module.GetNodeFullName(mod.GetMajorNameSanitised(), componentInfo.GetResourceName()),
			Labels: map[string]string{
				v1alpha1.FlowIDLabel:        "", //<-- empty flow means that's a node for palette
				v1alpha1.ModuleNameLabel:    mod.GetMajorNameSanitised(),
				v1alpha1.ModuleVersionLabel: mod.Version,
			},
			Annotations: map[string]string{
				v1alpha1.ComponentDescriptionAnnotation: componentInfo.Description,
				v1alpha1.ComponentInfoAnnotation:        componentInfo.Info,
				v1alpha1.ComponentTagsAnnotation:        strings.Join(componentInfo.Tags, ","),
			},
		},
		Spec: v1alpha1.TinyNodeSpec{
			Module:    mod.GetMajorNameSanitised(),
			Component: utils.SanitizeResourceName(c.GetInfo().Name),
			Run:       false,
		},
	}

	err := m.client.Create(ctx, node)
	if errors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (m Resource) Start(ctx context.Context) error {
	<-ctx.Done()
	return nil
}
