package utils

import (
	"fmt"

	"github.com/mgoltzsche/k8storagex/internal/template"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func Substitute(o runtime.Object, values *template.Substitution) error {
	unstr, err := runtime.DefaultUnstructuredConverter.ToUnstructured(o)
	if err != nil {
		return fmt.Errorf("convert %T to unstructured: %w", o, err)
	}
	unstr, err = values.SubstituteMap(unstr)
	if err != nil {
		return fmt.Errorf("substitute: %w", err)
	}
	u := &unstructured.Unstructured{Object: unstr}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, o); err != nil {
		return fmt.Errorf("convert unstructured to %T: %w", o, err)
	}
	return nil
}
