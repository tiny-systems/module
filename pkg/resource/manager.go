package resource

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/rs/zerolog/log"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/utils"
	v1core "k8s.io/api/core/v1"
	v1ingress "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"os"
	"reflect"
	"regexp"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sync"
	"time"
)

type Manager struct {
	client    client.Client
	namespace string
	log       logr.Logger
	lock      *sync.Mutex
}

type ManagerInterface interface {
	RegisterModule(ctx context.Context, mod module.Info) error
	PatchNode(ctx context.Context, node v1alpha1.TinyNode, update func(node *v1alpha1.TinyNode) error) error
}

func NewManager(c client.Client, log logr.Logger, ns string) *Manager {
	return &Manager{client: c, log: log, namespace: ns, lock: &sync.Mutex{}}
}

func (m Manager) RegisterModule(ctx context.Context, mod module.Info) error {

	spec := v1alpha1.TinyModuleSpec{
		Image: mod.GetNameAndVersion(),
	}

	node := &v1alpha1.TinyModule{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   m.namespace, // @todo make dynamic
			Name:        mod.GetNameSanitised(),
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

func (m Manager) getReleaseNameByPodName(ctx context.Context, podName string) (string, error) {
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

func (m Manager) ExposePort(ctx context.Context, autoHostName string, hostnames []string, port int) ([]string, error) {
	m.log.Info("expose port", "port", port, "hostnames", hostnames)
	defer m.log.Info("expose port done", "port", port)

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

	m.lock.Lock()
	// protect service and ingress from the concurrent update
	defer m.lock.Unlock()

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
		hostnames = append(hostnames, fmt.Sprintf("%s-%s", autoHostName, prefix))
	}
	//
	return m.addRulesIngress(ctx, ingress, svc, hostnames, port)
}

func (m Manager) getIngressAutoHostnamePrefix(ctx context.Context, ingress *v1ingress.Ingress) string {

	var hostNamePrefix string
	for k, v := range ingress.Annotations {
		if k == v1alpha1.IngressHostNameSuffixAnnotation {
			hostNamePrefix = v
		}
	}
	return hostNamePrefix
}

func (m Manager) removeRulesIngress(ctx context.Context, ingress *v1ingress.Ingress, service *v1core.Service, port int) error {

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

	for _, t := range ingress.Spec.TLS {

		var found bool
		for _, h := range t.Hosts {
			for _, host := range deletedHostnames {
				if h == host {
					found = true
				}
			}
		}
		if found {
			// found deleted host, this TLS going to be deleted (skipped from being saved)
			continue
		}
		tls = append(tls, t)
	}

	ingress.Spec.TLS = tls
	ingress.Spec.Rules = rules
	return m.client.Update(ctx, ingress)
}

func (m Manager) addRulesIngress(ctx context.Context, ingress *v1ingress.Ingress, service *v1core.Service, hostnames []string, port int) ([]string, error) {

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
		for idx, r := range ingress.Spec.Rules {
			if r.Host != hostname {
				continue
			}
			ingress.Spec.Rules[idx].IngressRuleValue = rule
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

	if len(newHostNames) > 0 {
		for _, hostname := range newHostNames {
			ingress.Spec.TLS = append(ingress.Spec.TLS, v1ingress.IngressTLS{
				Hosts:      []string{hostname},
				SecretName: fmt.Sprintf("%s-tls", hostname),
			})
		}
	}

	if err := m.client.Update(ctx, ingress); err != nil {
		return []string{}, err
	}
	return hostnames, nil
}

func (m Manager) discloseServicePort(ctx context.Context, svc *v1core.Service, port int) error {
	var ports []v1core.ServicePort

	for _, p := range svc.Spec.Ports {
		if p.Port == int32(port) {
			continue
		}
		// save others
		ports = append(ports, p)
	}
	svc.Spec.Ports = ports
	return m.client.Update(ctx, svc)
}

func (m Manager) exposeServicePort(ctx context.Context, svc *v1core.Service, port int) error {
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

func (m Manager) getReleaseService(ctx context.Context, releaseName string) (*v1core.Service, error) {
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

func (m Manager) getReleaseIngress(ctx context.Context, releaseName string) (*v1ingress.Ingress, error) {
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

func (m Manager) DisclosePort(ctx context.Context, port int) error {
	m.log.Info("disclose port", "port", port)

	currentPod := os.Getenv("HOSTNAME")
	if currentPod == "" {
		m.log.Error(fmt.Errorf("unable to determine the current pod's name"), "HOSTNAME env is empty")
		return nil
	}

	releaseName, err := m.getReleaseNameByPodName(ctx, currentPod)
	if err != nil {
		return fmt.Errorf("unable to find release name: %v", err)
	}

	m.lock.Lock()
	defer m.lock.Unlock()
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

func (m Manager) PatchNode(ctx context.Context, node v1alpha1.TinyNode, updater func(node *v1alpha1.TinyNode) error) error {

	findNode := &v1alpha1.TinyNode{}
	if err := m.client.Get(ctx, client.ObjectKeyFromObject(&node), findNode); err != nil {
		return err
	}

	if err := updater(findNode); err != nil {
		return err
	}

	if reflect.DeepEqual(findNode.Status, node.Status) {
		m.log.Info("nothing to patch node with")
		return nil
	}

	log.Warn().Msgf("patching node %s", node.Name)
	//spew.Dump(node.Status.Ports)

	err := m.client.Status().Patch(ctx, findNode, client.MergeFrom(&node))
	if err != nil {
		log.Error().Err(err).Msgf("path error")
	}
	return err
}

func (m Manager) CreateFlow(ctx context.Context, ns string, projectName string, name string) (*string, error) {

	flow := v1alpha1.TinyFlow{
		ObjectMeta: metav1.ObjectMeta{
			Labels:       map[string]string{},
			Namespace:    ns,
			GenerateName: utils.SanitizeResourceName(name),
			Annotations: map[string]string{
				v1alpha1.FlowDescriptionAnnotation: name,
			},
		},
	}

	flow.Labels[v1alpha1.ProjectNameLabel] = projectName

	if err := m.client.Create(ctx, &flow); err != nil {
		return nil, fmt.Errorf("unable to create project %s", err)
	}
	return &flow.Name, nil
}

func (m Manager) CreateProject(ctx context.Context, ns string, name string) (*v1alpha1.TinyProject, error) {

	proj := v1alpha1.TinyProject{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    ns,
			GenerateName: utils.SanitizeResourceName(name),
			Annotations:  map[string]string{},
			Labels:       map[string]string{},
		},
	}

	proj.Annotations[v1alpha1.ProjectNameAnnotation] = name

	if err := m.client.Create(ctx, &proj); err != nil {
		return nil, fmt.Errorf("unable to create project %s", err)
	}

	return &proj, nil
}

func (m Manager) GetProjectList(ctx context.Context) ([]v1alpha1.TinyProject, error) {
	var projectList = &v1alpha1.TinyProjectList{}

	if err := m.client.List(ctx, projectList, client.InNamespace(m.namespace)); err != nil {
		return nil, fmt.Errorf("service list error: %v", err)
	}
	return projectList.Items, nil
}

func (m Manager) GetFlowList(ctx context.Context, projectName string) ([]v1alpha1.TinyFlow, error) {

	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{
			v1alpha1.ProjectNameLabel: projectName,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("build selector error: %s", err)
	}
	var projectList = &v1alpha1.TinyFlowList{}

	if err := m.client.List(ctx, projectList, client.MatchingLabelsSelector{
		Selector: selector,
	}, client.InNamespace(m.namespace)); err != nil {
		return nil, fmt.Errorf("service list error: %v", err)
	}
	return projectList.Items, nil
}

func (m Manager) CreatePage(ctx context.Context, page *v1alpha1.TinyWidgetPage) error {
	return m.client.Create(ctx, page)
}

func (m Manager) DeletePage(ctx context.Context, page *v1alpha1.TinyWidgetPage) error {
	return m.client.Delete(ctx, page)
}

func (m Manager) UpdatePage(ctx context.Context, page *v1alpha1.TinyWidgetPage) error {
	return m.client.Update(ctx, page)
}

func (m Manager) GetProjectNodes(ctx context.Context, projectName string) ([]v1alpha1.TinyNode, error) {
	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{
			v1alpha1.ProjectNameLabel: projectName,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("build selector error: %s", err)
	}

	var (
		list = &v1alpha1.TinyNodeList{}
	)
	err = m.client.List(ctx, list, client.MatchingLabelsSelector{
		Selector: selector,
	}, client.InNamespace(m.namespace))
	if err != nil {
		return nil, err
	}

	return list.Items, nil
}

func (m Manager) GetProjectPageWidgets(ctx context.Context, projectName string) ([]v1alpha1.TinyWidgetPage, error) {
	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{
			v1alpha1.ProjectNameLabel: projectName,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("build selector error: %s", err)
	}

	var (
		list = &v1alpha1.TinyWidgetPageList{}
	)
	err = m.client.List(ctx, list, client.MatchingLabelsSelector{
		Selector: selector,
	}, client.InNamespace(m.namespace))
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

func (m Manager) MoveWidgetPageToPos(ctx context.Context, namespace, pageResourceName, pos int) error {
	return nil
}

func (m Manager) DeleteTracker(ctx context.Context, tracker *v1alpha1.TinyTracker, c client.Client) error {
	log.Info().Msg("deleting tracker")
	delCtx, cancel := context.WithTimeout(ctx, time.Second*30)
	defer cancel()

	if err := c.Delete(delCtx, tracker); err != nil && !errors.IsNotFound(err) {
		return err
	}
	return nil
}

func (m Manager) RenameFlow(ctx context.Context, name string, namespace string, newName string) error {

	flow := &v1alpha1.TinyFlow{}
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	err := m.client.Get(ctx, key, flow)

	if err != nil {
		return err
	}
	flow.Annotations[v1alpha1.FlowDescriptionAnnotation] = newName
	return m.client.Update(ctx, flow)
}

func (m Manager) GetFLow(ctx context.Context, name string, namespace string) (*v1alpha1.TinyFlow, error) {

	flow := &v1alpha1.TinyFlow{}
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	err := m.client.Get(ctx, key, flow)

	if err != nil {
		return nil, err
	}
	return flow, nil
}

func (m Manager) PutTracker(ctx context.Context, namespace string, projectResourceName string, c client.Client) (*v1alpha1.TinyTracker, error) {
	// place tracker
	tracker := &v1alpha1.TinyTracker{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Labels: map[string]string{
				v1alpha1.ProjectNameLabel: projectResourceName,
			},
			Annotations:  map[string]string{},
			GenerateName: utils.SanitizeResourceName(fmt.Sprintf("prj-%s-tracker-", projectResourceName)),
		},
		Spec: v1alpha1.TinyTrackerSpec{},
	}

	if err := c.Create(ctx, tracker); err != nil {
		log.Error().Err(err).Msg("can not create tracker")
		return nil, fmt.Errorf("can not create tracker %s", err)
	}

	return tracker, nil
}

func (m Manager) Start(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

var whitespaceRegex = regexp.MustCompile(`\s+`)

func removeAllWhitespace(input string) string {
	// Replace all matches of the whitespace regex with an empty string
	return whitespaceRegex.ReplaceAllString(input, "")
}
