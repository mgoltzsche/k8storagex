package utils

import (
	storageapi "github.com/mgoltzsche/cache-provisioner/api/v1alpha1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func AnnotationsToEnv(o metav1.Object, mapping []storageapi.EnvVar) (env []corev1.EnvVar, err error) {
	env = make([]corev1.EnvVar, 0, len(mapping))
	a := o.GetAnnotations()
	for _, m := range mapping {
		var v string
		if a != nil {
			v = a[m.Annotation]
		}
		if v != "" {
			env = append(env, corev1.EnvVar{Name: m.Name, Value: v})
		} else if (m.Required == nil || *m.Required) && err == nil {
			err = errors.Errorf("missing or empty annotation %q", m.Annotation)
		}
	}
	return env, err
}

func CopyAnnotations(src, dest metav1.Object, mapping []storageapi.EnvVar) {
	a := src.GetAnnotations()
	b := dest.GetAnnotations()
	if b == nil {
		b = map[string]string{}
	}
	for _, m := range mapping {
		if a != nil {
			b[m.Annotation] = a[m.Annotation]
		}
	}
	dest.SetAnnotations(b)
}
