package manager

import (
	"context"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/utils"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

type Resource struct {
	client    client.Client
	namespace string
	mod       module.Info
}

func NewManager(c client.Client, ns string, mod module.Info) *Resource {
	return &Resource{client: c, namespace: ns, mod: mod}
}

func (m Resource) Cleanup(ctx context.Context) error {
	sel := labels.NewSelector()

	req, err := labels.NewRequirement(v1alpha1.FlowIDLabel, selection.Equals, []string{""})
	if err != nil {
		return err
	}
	sel = sel.Add(*req)

	req, err = labels.NewRequirement(v1alpha1.ModuleNameLabel, selection.Equals, []string{m.mod.GetMajorNameSanitised()})
	if err != nil {
		return err
	}
	sel = sel.Add(*req)

	req, err = labels.NewRequirement(v1alpha1.ModuleVersionLabel, selection.NotEquals, []string{m.mod.Version})
	if err != nil {
		return err
	}
	sel = sel.Add(*req)

	return m.client.DeleteAllOf(ctx, &v1alpha1.TinyNode{}, client.InNamespace(m.namespace), client.MatchingLabelsSelector{
		Selector: sel,
	})
}

func (m Resource) RegisterModule(ctx context.Context) error {

	spec := v1alpha1.TinyModuleSpec{
		Image: m.mod.GetFullName(),
	}

	node := &v1alpha1.TinyModule{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   m.namespace, // @todo make dynamic
			Name:        m.mod.GetMajorNameSanitised(),
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

func (m Resource) RegisterComponent(ctx context.Context, c module.Component) error {

	componentInfo := c.GetInfo()
	node := &v1alpha1.TinyNode{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: m.namespace, // @todo make dynamic
			Name:      module.GetNodeFullName(m.mod.GetMajorNameSanitised(), componentInfo.GetResourceName()),
			Labels: map[string]string{
				v1alpha1.FlowIDLabel:        "", //<-- empty flow means that's a node for palette
				v1alpha1.ModuleNameLabel:    m.mod.GetMajorNameSanitised(),
				v1alpha1.ModuleVersionLabel: m.mod.Version,
			},
			Annotations: map[string]string{
				v1alpha1.ComponentDescriptionAnnotation: componentInfo.Description,
				v1alpha1.ComponentInfoAnnotation:        componentInfo.Info,
				v1alpha1.ComponentTagsAnnotation:        strings.Join(componentInfo.Tags, ","),
			},
		},
		Spec: v1alpha1.TinyNodeSpec{
			Module:    m.mod.GetMajorNameSanitised(),
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
