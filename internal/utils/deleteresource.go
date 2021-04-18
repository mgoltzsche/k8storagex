package utils

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func DeleteResource(ctx context.Context, client client.Client, o client.Object, log logr.Logger) (done bool, err error) {
	err = client.Delete(ctx, o)
	if apierrors.IsNotFound(err) {
		return true, nil
	}
	kind := o.GetObjectKind().GroupVersionKind().Kind
	if err == nil {
		log.Info(fmt.Sprintf("Deleted %s", kind), strings.ToLower(kind), fmt.Sprintf("%s/%s", o.GetNamespace(), o.GetName()))
		return false, nil
	}
	return false, err
}
