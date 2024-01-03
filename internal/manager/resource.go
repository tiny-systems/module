package manager

import (
	"context"
	"fmt"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/utils"
	v1core "k8s.io/api/core/v1"
	v1ingress "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/intstr"

	"os"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Resource struct {
	client    client.Client
	namespace string
}

type ResourceInterface interface {
	CleanupExampleNodes(ctx context.Context, mod module.Info) error
	RegisterModule(ctx context.Context, mod module.Info) error
	ExposePort(ctx context.Context, autoHostName string, hostnames []string, port int) ([]string, error)
	DisclosePort(ctx context.Context, port int) error
	RegisterExampleNode(ctx context.Context, c module.Component, mod module.Info) error
}

func NewManager(c client.Client, ns string) *Resource {
	return &Resource{client: c, namespace: ns}
}

// CleanupExampleNodes  @todo deal with it later
func (m Resource) CleanupExampleNodes(ctx context.Context, mod module.Info) error {
	sel := labels.NewSelector()

	req, err := labels.NewRequirement(v1alpha1.FlowIDLabel, selection.Equals, []string{""})
	if err != nil {
		return err
	}
	sel = sel.Add(*req)

	req, err = labels.NewRequirement(v1alpha1.ModuleNameMajorLabel, selection.Equals, []string{mod.GetMajorNameSanitised()})
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
	return nil
}

func (m Resource) RegisterModule(ctx context.Context, mod module.Info) error {

	spec := v1alpha1.TinyModuleSpec{
		Image: mod.GetNameAndVersion(),
	}

	node := &v1alpha1.TinyModule{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: m.namespace, // @todo make dynamic
			Name:      mod.GetMajorNameSanitised(),
			Labels:    map[string]string{
				//	v1alpha1.ModuleNameLabel: mod.Name,
			},
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

func (m Resource) ExposePort(ctx context.Context, autoHostName string, hostnames []string, port int) ([]string, error) {
	currentPod := os.Getenv("HOSTNAME")
	if currentPod == "" {
		return []string{}, fmt.Errorf("unable to determine the current pod's name")
	}

	if hostnames == nil {
		hostnames = []string{}
	}

	if len(hostnames) == 0 && autoHostName == "" {
		return []string{}, fmt.Errorf("empty hostnames provided")
	}

	pod := &v1core.Pod{}
	err := m.client.Get(context.Background(), client.ObjectKey{
		Namespace: m.namespace,
		Name:      currentPod,
	}, pod)

	if err != nil {
		return []string{}, fmt.Errorf("unable to find current pod: %v", err)
	}

	var releaseName string
	for k, v := range pod.ObjectMeta.Labels {
		if k == "app.kubernetes.io/instance" {
			releaseName = v
		}
	}
	if releaseName == "" {
		return []string{}, fmt.Errorf("release name label not found")
	}

	svc, err := m.getReleaseService(ctx, releaseName)
	if err != nil {
		return []string{}, fmt.Errorf("unable to get service: %v", err)
	}

	ingress, _ := m.getReleaseIngress(ctx, releaseName)

	if ingress == nil {
		return []string{}, fmt.Errorf("no ingress")
	}

	if err = m.exposeServicePort(ctx, svc, port); err != nil {
		return []string{}, err
	}

	prefix := m.getIngressAutoHostnamePrefix(ctx, ingress)

	if prefix != "" && autoHostName != "" {
		hostnames = append(hostnames, fmt.Sprintf("%s-%s%s", autoHostName, svc.Namespace, prefix))
	}
	return m.updateIngress(ctx, ingress, svc, hostnames, port)
}

func (m Resource) getIngressAutoHostnamePrefix(ctx context.Context, ingress *v1ingress.Ingress) string {

	var hostNamePrefix string
	for k, v := range ingress.Annotations {
		if k == v1alpha1.IngressHostNameSuffixAnnotation {
			hostNamePrefix = v
		}
	}
	return hostNamePrefix
}

func (m Resource) updateIngress(ctx context.Context, ingress *v1ingress.Ingress, service *v1core.Service, hostnames []string, port int) ([]string, error) {

	if len(hostnames) == 0 {
		return []string{}, fmt.Errorf("no hostnames provided")
	}

	for _, hostname := range hostnames {

		var found bool
		for _, rule := range ingress.Spec.Rules {
			if rule.Host == hostname && rule.IngressRuleValue.HTTP != nil {
				for _, p := range rule.IngressRuleValue.HTTP.Paths {
					if p.Backend.Service.Port.Number == int32(port) {
						p.Backend.Service.Name = service.Name
						found = true
					}
				}
				// update rule for the given host
			}
		}
		if !found {
			pathType := v1ingress.PathTypePrefix
			ingress.Spec.Rules = append(ingress.Spec.Rules, v1ingress.IngressRule{
				Host: hostname,
				IngressRuleValue: v1ingress.IngressRuleValue{
					HTTP: &v1ingress.HTTPIngressRuleValue{
						Paths: []v1ingress.HTTPIngressPath{
							{
								Path:     "/",
								PathType: &pathType,
								Backend: v1ingress.IngressBackend{
									Service: &v1ingress.IngressServiceBackend{
										Name: service.Name,
										Port: v1ingress.ServiceBackendPort{
											Number: int32(port),
										},
									},
								},
							},
						},
					},
				},
			})
		}
	}

	if err := m.client.Update(ctx, ingress); err != nil {
		return []string{}, err
	}
	return hostnames, nil
}

func (m Resource) exposeServicePort(ctx context.Context, svc *v1core.Service, port int) error {
	for _, p := range svc.Spec.Ports {
		if p.Port == int32(port) {
			// service has port already exposed
			return nil
		}
	}
	svc.Spec.Ports = append(svc.Spec.Ports, v1core.ServicePort{
		Name:       fmt.Sprintf("port%d", port),
		Port:       int32(port),
		TargetPort: intstr.FromInt32(int32(port)),
	})
	return m.client.Update(ctx, svc)
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
			Name:      module.GetNodeFullName("00000000", mod.GetMajorNameSanitised(), componentInfo.GetResourceName()),
			Labels: map[string]string{
				v1alpha1.FlowIDLabel:          "", //<-- empty flow means that's a node for palette
				v1alpha1.ModuleNameMajorLabel: mod.GetMajorNameSanitised(),
				v1alpha1.ModuleVersionLabel:   mod.Version,
			},
		},
		Spec: v1alpha1.TinyNodeSpec{
			Module:    mod.GetMajorNameSanitised(),
			Component: utils.SanitizeResourceName(c.GetInfo().Name),
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
