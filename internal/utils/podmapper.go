package utils

import (
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func PodToRequestMapper(namespace, annName, annNamespace string) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(o client.Object) []reconcile.Request {
		if o.GetNamespace() != namespace {
			return nil
		}
		a := o.GetAnnotations()
		if a == nil {
			return nil
		}
		owner := types.NamespacedName{
			Name:      a[annName],
			Namespace: a[annNamespace],
		}
		if owner.Name == "" || (owner.Namespace == "" && annNamespace != "") {
			return nil
		}
		return []reconcile.Request{{NamespacedName: owner}}
	})
}
