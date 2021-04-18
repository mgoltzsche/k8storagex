package utils

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	kerr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ReconcilePod reconciles a Pod
func ReconcilePod(ctx context.Context, client client.Client, name types.NamespacedName, create func() (*corev1.Pod, error)) (done bool, pod *corev1.Pod, err error) {
	pod = &corev1.Pod{}
	err = client.Get(ctx, name, pod)
	if err != nil {
		if kerr.IsNotFound(err) {
			// Create Pod if it doesn't exist
			if pod, err = create(); err != nil {
				return false, pod, err
			}
			pod.Name = name.Name
			pod.Namespace = name.Namespace
			err = client.Create(ctx, pod)
			if err == nil {
				return false, pod, nil // Pod created, watch will invoke another reconcile run
			}
			if apierrors.IsAlreadyExists(err) {
				// Reload Pod immediately to avoid polluting the log
				err = client.Get(ctx, name, pod)
			}
		}
		if err != nil {
			return false, pod, errors.Wrap(err, "reconcile pod")
		}
	}
	if pod.Status.Phase == corev1.PodSucceeded {
		return true, pod, nil
	}
	if pod.Status.Phase == corev1.PodFailed {
		return true, pod, errors.Errorf("pod %s failed", pod.Name)
	}
	return false, pod, nil
}

// TODO: use this in all controllers to run de/provisioner Pods
type JobReconciler struct {
	Client                   client.Client
	Recorder                 record.EventRecorder
	AnnotationOwnerName      string
	AnnotationOwnerNamespace string
}

type JobRequest struct {
	Context     context.Context
	Name        string
	PodName     types.NamespacedName
	Owner       client.Object
	ShouldRun   bool
	Create      func() (*corev1.Pod, error)
	OnCompleted func(*corev1.Pod) (done bool, err error)
	Log         logr.Logger
}

func (r *JobReconciler) ReconcileJob(req JobRequest) (bool, error) {
	podLog := req.Log.WithValues("pod", req.PodName.String())
	if !req.ShouldRun {
		return DeletePod(req.Context, r.Client, req.PodName, req.Log)
	}
	done, pod, err := ReconcilePod(req.Context, r.Client, req.PodName, func() (*corev1.Pod, error) {
		pod, err := req.Create()
		if err != nil {
			return nil, err
		}
		if pod.Annotations == nil {
			pod.Annotations = map[string]string{}
		}
		pod.Annotations[r.AnnotationOwnerName] = req.Owner.GetName()
		if r.AnnotationOwnerNamespace != "" {
			pod.Annotations[r.AnnotationOwnerNamespace] = req.Owner.GetNamespace()
		}
		pod.Spec.RestartPolicy = corev1.RestartPolicyNever

		logWithNode(podLog, pod).Info(fmt.Sprintf("Creating %s Pod", req.Name))

		return pod, nil
	})
	nameUpper := fmt.Sprintf("%s%s", strings.ToUpper(req.Name[0:1]), req.Name[1:])
	err = errors.Wrap(err, req.Name)
	if err != nil || !done {
		if err != nil && done {
			reason := fmt.Sprintf("%sFailed", nameUpper)
			msg := fmt.Sprintf("%s Pod failed", nameUpper)
			logWithNode(podLog, pod).Error(err, msg)
			r.Recorder.Event(req.Owner, corev1.EventTypeWarning, reason, msg)
		}
		return done, err
	}

	reason := fmt.Sprintf("%sCompleted", nameUpper)
	msg := fmt.Sprintf("%s Pod completed", nameUpper)
	logWithNode(podLog, pod).Info(msg)
	r.Recorder.Event(req.Owner, corev1.EventTypeNormal, reason, msg)

	// Run success callback (before Pod deletion in order to prevent the pod from being recreated again during the reconciliation)
	done, err = req.OnCompleted(pod)
	if err != nil || !done {
		return done, err
	}

	// Delete pod
	return DeleteResource(req.Context, r.Client, pod, req.Log)
}

func logWithNode(log logr.Logger, pod *corev1.Pod) logr.Logger {
	if pod.Spec.NodeName == "" {
		return log
	}
	return log.WithValues("node", pod.Spec.NodeName)
}

func DeletePod(ctx context.Context, client client.Client, name types.NamespacedName, log logr.Logger) (done bool, err error) {
	pod := &corev1.Pod{}
	err = client.Get(ctx, name, pod)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}
	return DeleteResource(ctx, client, pod, log)
}
