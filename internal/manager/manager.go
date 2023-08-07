package manager

import (
	"context"
	"fmt"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

type Manager struct {
	client    client.Client
	namespace string
	mod       module.Info
}

func NewManager(c client.Client, ns string, mod module.Info) *Manager {
	return &Manager{client: c, namespace: ns, mod: mod}
}

func (m Manager) UninstallPrevious(ctx context.Context, info module.Info) error {
	sel := labels.NewSelector()

	req, err := labels.NewRequirement(v1alpha1.FlowIDLabel, selection.Equals, []string{""})
	if err != nil {
		return err
	}
	sel = sel.Add(*req)

	req, err = labels.NewRequirement(v1alpha1.ModuleNameLabel, selection.Equals, []string{info.Name})
	if err != nil {
		return err
	}
	sel = sel.Add(*req)

	req, err = labels.NewRequirement(v1alpha1.ModuleVersionLabel, selection.NotEquals, []string{info.Version})
	if err != nil {
		return err
	}
	sel = sel.Add(*req)

	return m.client.DeleteAllOf(ctx, &v1alpha1.TinyNode{}, client.InNamespace("tinysystems"), client.MatchingLabelsSelector{
		Selector: sel,
	})
}

func (m Manager) Register(ctx context.Context, c module.Component) error {

	spec := v1alpha1.TinyNodeSpec{
		Module:    m.mod.GetFullName(),
		Component: c.GetInfo().Name,
		Run:       false,
	}
	componentInfo := c.GetInfo()

	node := &v1alpha1.TinyNode{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: m.namespace, // @todo make dynamic
			Name:      utils.SanitizeResourceName(fmt.Sprintf("%s-%s", m.mod.GetFullName(), componentInfo.Name)),
			Labels: map[string]string{
				v1alpha1.FlowIDLabel:        "", //<-- empty flow means that's a node for palette
				v1alpha1.ModuleNameLabel:    m.mod.Name,
				v1alpha1.ModuleVersionLabel: m.mod.Version,
			},
			Annotations: map[string]string{
				v1alpha1.ComponentNameAnnotation:        componentInfo.Name,
				v1alpha1.ComponentDescriptionAnnotation: componentInfo.Description,
				v1alpha1.ComponentInfoAnnotation:        componentInfo.Info,
				v1alpha1.ComponentTagsAnnotation:        strings.Join(componentInfo.Tags, ","),
			},
		},
		Spec: spec,
	}
	return m.client.Create(ctx, node)
}

// Start
func (m Manager) Start(ctx context.Context) error {
	<-ctx.Done()
	return nil
}
