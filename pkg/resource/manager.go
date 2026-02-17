package resource

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"


	helmclient "github.com/mittwald/go-helm-client"
	"github.com/rs/zerolog/log"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/utils"
	"helm.sh/helm/v3/pkg/release"
	v1core "k8s.io/api/core/v1"
	v1ingress "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Manager struct {
	client    client.WithWatch
	config    *rest.Config
	namespace string
	lock      *sync.Mutex
}

type ManagerInterface interface {
	CreateModule(ctx context.Context, mod module.Info) error
	PatchNode(ctx context.Context, node v1alpha1.TinyNode, update func(node *v1alpha1.TinyNode) error) error
	CreateNode(ctx context.Context, node *v1alpha1.TinyNode) error
	UpdateNode(ctx context.Context, node *v1alpha1.TinyNode) error
	DeleteNode(ctx context.Context, node *v1alpha1.TinyNode) error
	GetNode(ctx context.Context, name, namespace string) (*v1alpha1.TinyNode, error)
	CreateSignal(ctx context.Context, nodeName, nodeNamespace string, port string, data []byte) error
}

func NewManagerFromClient(c client.WithWatch, ns string) (*Manager, error) {

	return &Manager{client: c, namespace: ns, lock: &sync.Mutex{}}, nil
}

// GetK8sClient returns the underlying Kubernetes client
func (m Manager) GetK8sClient() client.WithWatch {
	return m.client
}

// GetNamespace returns the configured namespace
func (m Manager) GetNamespace() string {
	return m.namespace
}

// GetRestConfig returns the rest config used to create the manager
func (m Manager) GetRestConfig() *rest.Config {
	return m.config
}

func NewManagerFromConfig(config *rest.Config, ns string) (*Manager, error) {

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = v1alpha1.AddToScheme(scheme)

	kubeClient, err := client.NewWithWatch(config, client.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}
	return &Manager{client: kubeClient, config: config, namespace: ns, lock: &sync.Mutex{}}, nil
}

func (m Manager) GetNode(ctx context.Context, name, namespace string) (*v1alpha1.TinyNode, error) {

	node := &v1alpha1.TinyNode{}

	if err := m.client.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}, node); err != nil {
		return nil, err
	}

	// Ensure APIVersion and Kind are set for owner references
	if node.APIVersion == "" {
		node.APIVersion = v1alpha1.GroupVersion.String()
	}
	if node.Kind == "" {
		node.Kind = "TinyNode"
	}

	return node, nil
}
func (m Manager) CreateNode(ctx context.Context, node *v1alpha1.TinyNode) error {
	// Set owner reference to the flow if the node belongs to a flow
	if flowName, ok := node.Labels[v1alpha1.FlowNameLabel]; ok && flowName != "" {
		flow, err := m.GetFLow(ctx, flowName, node.Namespace)
		if err != nil {
			return fmt.Errorf("failed to get flow for owner reference: %w", err)
		}

		// Set owner reference to the flow
		node.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion: flow.APIVersion,
				Kind:       flow.Kind,
				Name:       flow.Name,
				UID:        flow.UID,
				Controller: func() *bool { b := true; return &b }(),
			},
		}
	}

	return m.client.Create(ctx, node)
}

func (m Manager) UpdateNode(ctx context.Context, node *v1alpha1.TinyNode) error {
	return m.client.Update(ctx, node)
}

func (m Manager) CreateModule(ctx context.Context, mod module.Info) error {

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

	currentPod := os.Getenv("HOSTNAME")
	if currentPod == "" {
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

	// Save the current state from Kubernetes BEFORE modification to use as merge base.
	// Using a deep copy ensures the merge patch is computed against the actual current
	// state, not a stale cached 'node' which could cause empty patches when the updated
	// state matches the stale cached state but differs from the actual Kubernetes state.
	baseNode := findNode.DeepCopy()

	if err := updater(findNode); err != nil {
		return err
	}

	if reflect.DeepEqual(findNode.Status, baseNode.Status) {
		return nil
	}

	log.Warn().Msgf("patching node %s", node.Name)

	err := m.client.Status().Patch(ctx, findNode, client.MergeFrom(baseNode))
	if err != nil {
		log.Error().Err(err).Msgf("patch error")
	}
	return err
}

func (m Manager) CreateFlow(ctx context.Context, ns string, projectName string, name string) (*string, error) {

	if projectName == "" {
		return nil, fmt.Errorf("project name is required")
	}
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

	// Set owner reference to the project
	// Get the project by its resource name
	project, err := m.GetProject(ctx, projectName, ns)
	if err != nil {
		return nil, fmt.Errorf("failed to get project for owner reference: %w", err)
	}

	// Set owner reference to the project
	flow.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: project.APIVersion,
			Kind:       project.Kind,
			Name:       project.Name,
			UID:        project.UID,
			Controller: func() *bool { b := true; return &b }(),
		},
	}

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

func (m Manager) CreateSignal(ctx context.Context, nodeName, nodeNamespace string, port string, data []byte) error {

	// Get the target node to set as owner
	node, err := m.GetNode(ctx, nodeName, nodeNamespace)
	if err != nil {
		return fmt.Errorf("failed to get node for owner reference: %w", err)
	}

	signal := &v1alpha1.TinySignal{}
	name := fmt.Sprintf("%s-%s", nodeName, strings.ReplaceAll(port, "_", ""))

	err = m.client.Get(ctx, client.ObjectKey{Namespace: nodeNamespace, Name: name}, signal)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	newSignal := signal.DeepCopy()

	newSignal.Namespace = nodeNamespace
	newSignal.Name = name

	newSignal.Labels = map[string]string{
		v1alpha1.NodeNameLabel: nodeName,
	}

	// Set owner reference to the node
	newSignal.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: node.APIVersion,
			Kind:       node.Kind,
			Name:       node.Name,
			UID:        node.UID,
			Controller: func() *bool { b := true; return &b }(),
		},
	}

	newSignal.Spec = v1alpha1.TinySignalSpec{
		Node: nodeName,
		Port: port,
		Data: data,
	}

	if errors.IsNotFound(err) {
		if err := m.client.Create(ctx, newSignal); err != nil {
			return err
		}
		return nil
	}

	err = m.client.Patch(ctx, newSignal, client.MergeFrom(signal))
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	return nil
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

func (m Manager) CreatePage(ctx context.Context, requestedPageMame, projectName, namespace string, idx int) (*string, error) {

	requestedPage := &v1alpha1.TinyWidgetPage{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace,
			GenerateName: utils.SanitizeResourceName(requestedPageMame),
			Labels: map[string]string{
				v1alpha1.ProjectNameLabel: projectName,
			},
			Annotations: map[string]string{
				v1alpha1.PageTitleAnnotation:   requestedPageMame,
				v1alpha1.PageSortIdxAnnotation: strconv.Itoa(idx),
			},
		},
	}

	// Set owner reference to the project
	// Get the project by its resource name
	project, err := m.GetProject(ctx, projectName, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get project for owner reference: %w", err)
	}

	// Set owner reference to the project
	requestedPage.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: project.APIVersion,
			Kind:       project.Kind,
			Name:       project.Name,
			UID:        project.UID,
			Controller: func() *bool { b := true; return &b }(),
		},
	}

	if err := m.client.Create(ctx, requestedPage); err != nil {
		return nil, err
	}
	return &requestedPage.Name, nil
}

func (m Manager) DeletePage(ctx context.Context, page *v1alpha1.TinyWidgetPage) error {
	return m.client.Delete(ctx, page)
}

func (m Manager) UpdatePage(ctx context.Context, page *v1alpha1.TinyWidgetPage) error {
	return m.client.Update(ctx, page)
}

func (m Manager) GetProjectFlowNodes(ctx context.Context, projectName string, flowName string) ([]v1alpha1.TinyNode, error) {
	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{
			v1alpha1.ProjectNameLabel: projectName,
			v1alpha1.FlowNameLabel:    flowName,
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

func (m Manager) WatchNodes(ctx context.Context, projectResourceName string) (watch.Interface, error) {

	var (
		list = &v1alpha1.TinyNodeList{}
	)

	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{
			v1alpha1.ProjectNameLabel: projectResourceName,
		},
	})

	if err != nil {
		return nil, fmt.Errorf("build selector error: %s", err)
	}

	watcher, err := m.client.Watch(ctx, list, client.MatchingLabelsSelector{Selector: selector}, client.InNamespace(m.namespace))
	if err != nil {
		return nil, err
	}

	return watcher, nil
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

	// Ensure APIVersion and Kind are set for owner references
	if flow.APIVersion == "" {
		flow.APIVersion = v1alpha1.GroupVersion.String()
	}
	if flow.Kind == "" {
		flow.Kind = "TinyFlow"
	}

	return flow, nil
}

func (m Manager) GetProject(ctx context.Context, name string, namespace string) (*v1alpha1.TinyProject, error) {

	project := &v1alpha1.TinyProject{}
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	err := m.client.Get(ctx, key, project)

	if err != nil {
		return nil, err
	}

	// Ensure APIVersion and Kind are set for owner references
	if project.APIVersion == "" {
		project.APIVersion = v1alpha1.GroupVersion.String()
	}
	if project.Kind == "" {
		project.Kind = "TinyProject"
	}

	return project, nil
}

func (m Manager) UpdateProjectDescription(ctx context.Context, name string, namespace string, description string) error {
	project := &v1alpha1.TinyProject{}
	key := types.NamespacedName{Name: name, Namespace: namespace}
	if err := m.client.Get(ctx, key, project); err != nil {
		return err
	}
	project.Spec.Description = description
	return m.client.Update(ctx, project)
}

func (m Manager) RenameProject(ctx context.Context, name string, namespace string, newName string) error {

	project := &v1alpha1.TinyProject{}
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	err := m.client.Get(ctx, key, project)

	if err != nil {
		return err
	}

	if project.Annotations == nil {
		project.Annotations = make(map[string]string)
	}
	project.Annotations[v1alpha1.ProjectNameAnnotation] = newName
	return m.client.Update(ctx, project)
}

func (m Manager) DeleteFlow(ctx context.Context, flowResourceName string) error {

	if err := m.client.Delete(ctx, &v1alpha1.TinyFlow{
		ObjectMeta: metav1.ObjectMeta{
			Name:      flowResourceName,
			Namespace: m.namespace,
		},
	}); err != nil {
		return err
	}
	return nil
}
func (m Manager) DeleteProject(ctx context.Context, projectName string) error {

	if err := m.client.Delete(ctx, &v1alpha1.TinyProject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      projectName,
			Namespace: m.namespace,
		},
	}); err != nil {
		return err
	}
	return nil
}

func (m Manager) DeleteNode(ctx context.Context, n *v1alpha1.TinyNode) error {
	return m.client.Delete(ctx, n)
}

// CleanupNodeReferences removes edges and port configs referencing a deleted
// node from all remaining nodes in the project. Call after DeleteNode succeeds.
func (m Manager) CleanupNodeReferences(ctx context.Context, projectName, deletedNodeName string) error {
	nodes, err := m.GetProjectNodes(ctx, projectName)
	if err != nil {
		return fmt.Errorf("get project nodes: %w", err)
	}
	for i := range nodes {
		if nodes[i].Name == deletedNodeName {
			continue
		}
		if utils.RemoveNodeReferences(&nodes[i], deletedNodeName) {
			if err := m.UpdateNode(ctx, &nodes[i]); err != nil {
				log.Warn().Err(err).Str("node", nodes[i].Name).Msg("failed to clean up edges after node deletion")
			}
		}
	}
	return nil
}

// CleanupWidgetReferences removes widgets referencing a deleted node
// from all widget pages in the project. Call after DeleteNode succeeds.
func (m Manager) CleanupWidgetReferences(ctx context.Context, projectName, deletedNodeName string) error {
	pages, err := m.GetProjectPageWidgets(ctx, projectName)
	if err != nil {
		return fmt.Errorf("get widget pages: %w", err)
	}
	prefix := deletedNodeName + ":"
	for i := range pages {
		filtered := make([]v1alpha1.TinyWidget, 0, len(pages[i].Spec.Widgets))
		for _, w := range pages[i].Spec.Widgets {
			if !strings.HasPrefix(w.Port, prefix) {
				filtered = append(filtered, w)
			}
		}
		if len(filtered) != len(pages[i].Spec.Widgets) {
			pages[i].Spec.Widgets = filtered
			if err := m.UpdatePage(ctx, &pages[i]); err != nil {
				log.Warn().Err(err).Str("page", pages[i].Name).Msg("failed to clean up widgets after node deletion")
			}
		}
	}
	return nil
}

// CleanupFlowWidgetReferences removes widgets referencing any node belonging
// to the given flow from all widget pages in the project.
// Call before DeleteFlow to prevent dangling widgets.
func (m Manager) CleanupFlowWidgetReferences(ctx context.Context, projectName, flowResourceName string) error {
	// List nodes belonging to this flow
	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{
			v1alpha1.ProjectNameLabel: projectName,
			v1alpha1.FlowNameLabel:    flowResourceName,
		},
	})
	if err != nil {
		return fmt.Errorf("build selector: %w", err)
	}
	list := &v1alpha1.TinyNodeList{}
	if err := m.client.List(ctx, list, client.MatchingLabelsSelector{Selector: selector}, client.InNamespace(m.namespace)); err != nil {
		return fmt.Errorf("list flow nodes: %w", err)
	}
	if len(list.Items) == 0 {
		return nil
	}

	// Build set of node name prefixes
	prefixes := make([]string, len(list.Items))
	for i, node := range list.Items {
		prefixes[i] = node.Name + ":"
	}

	// Get widget pages and filter out matching widgets
	pages, err := m.GetProjectPageWidgets(ctx, projectName)
	if err != nil {
		return fmt.Errorf("get widget pages: %w", err)
	}
	for i := range pages {
		filtered := make([]v1alpha1.TinyWidget, 0, len(pages[i].Spec.Widgets))
		for _, w := range pages[i].Spec.Widgets {
			keep := true
			for _, prefix := range prefixes {
				if strings.HasPrefix(w.Port, prefix) {
					keep = false
					break
				}
			}
			if keep {
				filtered = append(filtered, w)
			}
		}
		if len(filtered) != len(pages[i].Spec.Widgets) {
			pages[i].Spec.Widgets = filtered
			if err := m.UpdatePage(ctx, &pages[i]); err != nil {
				log.Warn().Err(err).Str("page", pages[i].Name).Msg("failed to clean up widgets after flow deletion")
			}
		}
	}
	return nil
}

func (m Manager) DeleteFlowNodes(ctx context.Context, projectResourceName string, flowResourceName string) error {

	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{
			v1alpha1.ProjectNameLabel: projectResourceName,
			v1alpha1.FlowNameLabel:    flowResourceName,
		},
	})
	if err != nil {
		return err
	}

	// cleanup nodes in flow
	if err = m.client.DeleteAllOf(ctx, &v1alpha1.TinyNode{}, client.MatchingLabelsSelector{Selector: selector}, client.InNamespace(m.namespace)); err != nil {
		return err
	}
	return nil
}

func (m Manager) GetInstalledComponents(ctx context.Context) ([]module.Info, error) {

	var list = &v1alpha1.TinyModuleList{}

	err := m.client.List(ctx, list, &client.ListOptions{
		Namespace: m.namespace,
	})

	if err != nil {
		return nil, fmt.Errorf("nodes list error: %s", err)
	}

	var modules []module.Info

	for _, v := range list.Items {

		mInfo := module.Info{
			Name:       v.Status.Name,
			Version:    v.Status.Version,
			Components: make([]module.ComponentInfo, 0),
		}

		for _, c := range v.Status.Components {
			mInfo.Components = append(mInfo.Components, module.ComponentInfo{
				Name:        c.Name,
				Description: c.Description,
				Info:        c.Info,
				Tags:        c.Tags,
			})
		}
		modules = append(modules, mInfo)
	}

	return modules, nil
}

func (m Manager) GetReleases(_ context.Context) ([]*release.Release, error) {

	options := &helmclient.Options{}
	helmClient, err := helmclient.NewClientFromRestConf(&helmclient.RestConfClientOptions{
		Options:    options,
		RestConfig: m.config,
	})
	if err != nil {
		return nil, err
	}
	releases, err := helmClient.ListDeployedReleases()
	if err != nil {
		return nil, err
	}

	return releases, nil
}

var _ ManagerInterface = (*Manager)(nil)

func (m Manager) Start(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

// DefaultSyncTimeout is the default timeout for waiting for node synchronization
const DefaultSyncTimeout = 30 * time.Second

// DefaultSyncPollInterval is the polling interval for sync checks
const DefaultSyncPollInterval = 500 * time.Millisecond

// CreateNodeSync creates a node and waits for it to be synchronized by the controller
func (m Manager) CreateNodeSync(ctx context.Context, node *v1alpha1.TinyNode, timeout time.Duration) error {
	if timeout == 0 {
		timeout = DefaultSyncTimeout
	}

	// Create the node first
	if err := m.CreateNode(ctx, node); err != nil {
		return err
	}

	// Wait for synchronization
	return m.WaitForNodeSync(ctx, node.Name, node.Namespace, timeout)
}

// UpdateNodeSync updates a node and waits for it to be synchronized by the controller
func (m Manager) UpdateNodeSync(ctx context.Context, node *v1alpha1.TinyNode, timeout time.Duration) error {
	if timeout == 0 {
		timeout = DefaultSyncTimeout
	}

	// Get current generation before update
	currentNode, err := m.GetNode(ctx, node.Name, node.Namespace)
	if err != nil {
		return fmt.Errorf("failed to get current node: %w", err)
	}
	prevGeneration := currentNode.Generation

	// Update the node
	if err := m.UpdateNode(ctx, node); err != nil {
		return err
	}

	// Wait for synchronization with generation check
	return m.waitForNodeSyncWithGeneration(ctx, node.Name, node.Namespace, prevGeneration, timeout)
}

// WaitForNodeSync waits for a node to be synchronized by the controller
// A node is considered synced when Status.Status == "OK", Status.Error == false,
// and Status.Module.Name is set (indicating controller processed it)
func (m Manager) WaitForNodeSync(ctx context.Context, name, namespace string, timeout time.Duration) error {
	if timeout == 0 {
		timeout = DefaultSyncTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return wait.PollUntilContextTimeout(ctx, DefaultSyncPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		node, err := m.GetNode(ctx, name, namespace)
		if err != nil {
			if errors.IsNotFound(err) {
				// Node might not exist yet, keep polling
				return false, nil
			}
			return false, err
		}

		// Check if controller has processed current generation
		if node.Status.ObservedGeneration >= node.Generation {
			// Controller processed this generation - check if it succeeded or failed
			if node.Status.Error {
				// Controller processed but component reported error (e.g., invalid settings)
				// Return early instead of polling until timeout
				return true, nil
			}
			// Check full sync status
			return isNodeSynced(node), nil
		}

		// Controller hasn't processed this generation yet
		return false, nil
	})
}

// waitForNodeSyncWithGeneration waits for a node to be synchronized after an update
// by checking that the controller has processed the new generation
func (m Manager) waitForNodeSyncWithGeneration(ctx context.Context, name, namespace string, prevGeneration int64, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return wait.PollUntilContextTimeout(ctx, DefaultSyncPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		node, err := m.GetNode(ctx, name, namespace)
		if err != nil {
			return false, err
		}

		// Check if generation has been updated and node is synced
		// For updates, we need to verify the controller processed the new spec
		if node.Generation > prevGeneration && isNodeSynced(node) {
			return true, nil
		}

		// If generation hasn't changed but spec was updated, still check sync status
		if isNodeSynced(node) {
			return true, nil
		}

		return false, nil
	})
}

// isNodeSynced checks if a node has been processed by the controller
func isNodeSynced(node *v1alpha1.TinyNode) bool {
	// Controller sets module name when it processes the node
	if node.Status.Module.Name == "" {
		return false
	}

	// Status should be "OK" (set by controller in Reconcile)
	if node.Status.Status != "OK" {
		return false
	}

	// No error
	if node.Status.Error {
		return false
	}

	// ObservedGeneration must match Generation to ensure controller processed current spec
	// This is critical for settings changes that affect ports (e.g., router routes)
	if node.Status.ObservedGeneration < node.Generation {
		return false
	}

	return true
}

// WaitForNodeDeletion waits for a node to be fully deleted
func (m Manager) WaitForNodeDeletion(ctx context.Context, name, namespace string, timeout time.Duration) error {
	if timeout == 0 {
		timeout = DefaultSyncTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return wait.PollUntilContextTimeout(ctx, DefaultSyncPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		_, err := m.GetNode(ctx, name, namespace)
		if err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	})
}

