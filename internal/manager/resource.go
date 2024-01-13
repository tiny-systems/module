package manager

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
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
	"strings"

	"os"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Resource struct {
	client    client.Client
	namespace string
	log       logr.Logger
}

type ResourceInterface interface {
	CleanupExampleNodes(ctx context.Context, mod module.Info) error
	RegisterModule(ctx context.Context, mod module.Info) error
	ExposePort(ctx context.Context, autoHostName string, hostnames []string, port int) ([]string, error)
	DisclosePort(ctx context.Context, port int) error
	RegisterExampleNode(ctx context.Context, c module.Component, mod module.Info) error
	CreateClusterNodeSignal(ctx context.Context, node *v1alpha1.TinyNode, port string, data []byte) error
}

func NewManager(c client.Client, log logr.Logger, ns string) *Resource {
	return &Resource{client: c, log: log, namespace: ns}
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

func (m Resource) getReleaseNameByPodName(ctx context.Context, podName string) (string, error) {
	pod := &v1core.Pod{}
	err := m.client.Get(context.Background(), client.ObjectKey{
		Namespace: m.namespace,
		Name:      podName,
	}, pod)

	if err != nil {
		return "", fmt.Errorf("unable to find current pod: %v", err)
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
	return releaseName, nil
}

func (m Resource) ExposePort(ctx context.Context, autoHostName string, hostnames []string, port int) ([]string, error) {
	m.log.Info("exposing port", "port", port, "hostnames", hostnames)

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

	releaseName, err := m.getReleaseNameByPodName(ctx, currentPod)
	if err != nil {
		return nil, fmt.Errorf("unable to find release name: %v", err)
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
	return m.addRulesIngress(ctx, ingress, svc, hostnames, port)
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

func (m Resource) removeRulesIngress(ctx context.Context, ingress *v1ingress.Ingress, service *v1core.Service, port int) error {

	var (
		rules            []v1ingress.IngressRule
		tls              []v1ingress.IngressTLS
		deletedHostnames []string
	)

RULES:
	for _, rule := range ingress.Spec.Rules {
		if rule.IngressRuleValue.HTTP == nil {
			continue
		}
		for _, p := range rule.IngressRuleValue.HTTP.Paths {
			if p.Backend.Service.Port.Number == int32(port) && p.Backend.Service.Name == service.Name {
				// omit saving
				deletedHostnames = append(deletedHostnames, rule.Host)
				continue RULES
			}
			rules = append(rules, rule)
		}
	}

TLS:
	for _, t := range tls {
		for _, h := range t.Hosts {
			for _, host := range deletedHostnames {
				if host != h {
					tls = append(tls, t)
					continue
				}
				continue TLS
			}
		}
	}

	ingress.Spec.TLS = tls
	ingress.Spec.Rules = rules
	return m.client.Update(ctx, ingress)
}

func (m Resource) addRulesIngress(ctx context.Context, ingress *v1ingress.Ingress, service *v1core.Service, hostnames []string, port int) ([]string, error) {

	if len(hostnames) == 0 {
		return []string{}, fmt.Errorf("no hostnames provided")
	}
	pathType := v1ingress.PathTypePrefix

	var rule = v1ingress.IngressRuleValue{
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
	}

INGRESS:
	for _, hostname := range hostnames {
		for _, r := range ingress.Spec.Rules {
			if r.Host != hostname {
				continue
			}
			r.IngressRuleValue = rule
			continue INGRESS
		}
		ingress.Spec.Rules = append(ingress.Spec.Rules, v1ingress.IngressRule{
			Host:             hostname,
			IngressRuleValue: rule,
		})
	}

	var newHostNames []string

HOSTNAMES:
	for _, hostname := range hostnames {
		for _, t := range ingress.Spec.TLS {
			for _, th := range t.Hosts {
				if th != hostname {
					continue
				}
				continue HOSTNAMES
			}
		}
		newHostNames = append(newHostNames, hostname)
	}

	if len(hostnames) > 0 {
		ingress.Spec.TLS = append(ingress.Spec.TLS, v1ingress.IngressTLS{
			Hosts:      hostnames,
			SecretName: fmt.Sprintf("%s-secret", ingress.Name),
		})
	}

	if err := m.client.Update(ctx, ingress); err != nil {
		return []string{}, err
	}
	return hostnames, nil
}

func (m Resource) discloseServicePort(ctx context.Context, svc *v1core.Service, port int) error {
	var ports []v1core.ServicePort

	for _, p := range svc.Spec.Ports {
		if p.Port != int32(port) {
			ports = append(ports, p)
			continue
		}
	}
	svc.Spec.Ports = ports
	return m.client.Update(ctx, svc)
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
	m.log.Info("disclose port", "port", port)

	currentPod := os.Getenv("HOSTNAME")
	if currentPod == "" {
		return fmt.Errorf("unable to determine the current pod's name")
	}

	releaseName, err := m.getReleaseNameByPodName(ctx, currentPod)
	if err != nil {
		return fmt.Errorf("unable to find release name: %v", err)
	}

	svc, err := m.getReleaseService(ctx, releaseName)
	if err != nil {
		return fmt.Errorf("unable to get service: %v", err)
	}

	ingress, _ := m.getReleaseIngress(ctx, releaseName)

	if ingress == nil {
		return fmt.Errorf("no ingress")
	}

	if err = m.discloseServicePort(ctx, svc, port); err != nil {
		return err
	}
	return m.removeRulesIngress(ctx, ingress, svc, port)
}

func (m Resource) CreateClusterNodeSignal(ctx context.Context, node *v1alpha1.TinyNode, port string, data []byte) error {
	signal := &v1alpha1.TinySignal{
		Spec: v1alpha1.TinySignalSpec{
			Node: node.Name,
			Port: port,
			Data: data,
		},
	}
	signal.Namespace = node.Namespace
	signal.GenerateName = fmt.Sprintf("%s-%s-", node.Name, strings.ReplaceAll(port, "_", ""))
	return m.client.Create(ctx, signal)
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
