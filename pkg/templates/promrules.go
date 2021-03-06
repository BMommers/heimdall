package templates

import (
	"bytes"
	"fmt"
	"html/template"
	"path/filepath"
	"strings"

	log "github.com/Sirupsen/logrus"

	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"

	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
)

var heimPrefix = "com.uswitch.heimdall"

// PrometheusRuleTemplateManager
// - contains a map of all the templates in the given templates folder
type PrometheusRuleTemplateManager struct {
	templates map[string]*template.Template
}

// templateParameter
// - struct passed to each promrule template
type templateParameter struct {
	Identifier string
	Threshold  string
	Namespace  string
	Name       string
	Host       string
	Value      string
	Ingress    *extensionsv1beta1.Ingress
}

// collectPrometheusRules
// - Accepts a map of PrometheusRules and returns Array
func collectPrometheusRules(prometheusRules map[string]*monitoringv1.PrometheusRule) []*monitoringv1.PrometheusRule {
	ret := make([]*monitoringv1.PrometheusRule, len(prometheusRules))

	idx := 0
	for _, v := range prometheusRules {
		ret[idx] = v
		idx = idx + 1
	}

	return ret
}

// NewPrometheusRuleTemplateManager
// - Creates a new PrometheusRuleTemplateManager taking a directory as a string
func NewPrometheusRuleTemplateManager(directory string) (*PrometheusRuleTemplateManager, error) {
	templates := map[string]*template.Template{}
	templateFiles, err := filepath.Glob(directory + "/*.tmpl")
	if err != nil {
		return nil, err
	}

	for _, t := range templateFiles {
		tmpl, err := template.ParseFiles(t)
		if err != nil {
			return nil, err
		}

		templates[strings.TrimRight(filepath.Base(t), ".tmpl")] = tmpl
	}

	if len(templates) == 0 {
		return nil, fmt.Errorf("no templates defined")
	}

	return &PrometheusRuleTemplateManager{templates}, nil
}

// CreateFromIngress
// - Creates all the promRules for a given Ingress
func (a *PrometheusRuleTemplateManager) CreateFromIngress(ingress *extensionsv1beta1.Ingress) ([]*monitoringv1.PrometheusRule, error) {
	ingressIdentifier := fmt.Sprintf("%s.%s", ingress.Namespace, ingress.Name)

	params := &templateParameter{
		Ingress:    ingress,
		Identifier: ingressIdentifier,
		Namespace:  ingress.Namespace,
		Name:       ingress.Name,
	}

	prometheusRules := map[string]*monitoringv1.PrometheusRule{}
	annotations := params.Ingress.GetAnnotations()

	for k, v := range annotations {
		if !strings.HasPrefix(k, heimPrefix) {
			continue
		}

		templateName := strings.TrimLeft(k, fmt.Sprintf("%s/", heimPrefix))
		template, ok := a.templates[templateName]
		if !ok {
			log.Warnf("[ingress][%s] no template for \"%s\"", ingressIdentifier, templateName)
			continue
		}

		params.Threshold = v
		var result bytes.Buffer
		if err := template.Execute(&result, params); err != nil {
			log.Warnf("[ingress][%s] error executing template : %s", ingressIdentifier, err)
			continue
		}

		promrule := &monitoringv1.PrometheusRule{}

		if err := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(result.Bytes()), 1024).Decode(promrule); err != nil {
			log.Warnf("[ingress][%s] error parsing YAML: %s", ingressIdentifier, err)
			continue
		}

		promrule.SetOwnerReferences([]metav1.OwnerReference{
			*metav1.NewControllerRef(ingress, schema.GroupVersionKind{
				Group:   extensionsv1beta1.SchemeGroupVersion.Group,
				Version: extensionsv1beta1.SchemeGroupVersion.Version,
				Kind:    "Ingress",
			}),
		})

		prometheusRules[promrule.ObjectMeta.Name] = promrule
	}

	return collectPrometheusRules(prometheusRules), nil
}
