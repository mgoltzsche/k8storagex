package controllers

import (
	"context"
	"time"

	storageapi "github.com/mgoltzsche/k8storagex/api/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

var (
	fakeProvisionerDeleteOnPodTermination = "delete-on-pod-termination.fake.provisioner"
	fakeProvisionerNoDeletion             = "ignore-terminating-pod.fake.provisioner"
)

var _ = Describe("PodController", func() {
	BeforeEach(func() {
		p := &storageapi.StorageProvisioner{}
		p.Name = "fake-provisioner"
		p.Spec.Name = fakeProvisionerNoDeletion
		p.Spec.PodTemplate.Containers = []corev1.Container{{
			Name:    "main",
			Image:   "fake-image",
			Command: []string{"true"},
		}}
		volMode := corev1.PersistentVolumeFilesystem
		p.Spec.PersistentVolumeTemplate.VolumeMode = &volMode
		p.Spec.Nodes = []storageapi.NodePath{{Name: "*", Path: "/fake/test/path"}}
		Expect(validateProvisioner(p)).To(BeNil())
		pDeleteOnPodTermination := *p
		pDeleteOnPodTermination.Name += "2"
		pDeleteOnPodTermination.Spec.Name = fakeProvisionerDeleteOnPodTermination
		pDeleteOnPodTermination.Spec.DeprovisionOnPodCompletion = true
		testProvisioners.Put(p)
		testProvisioners.Put(&pDeleteOnPodTermination)
	})
	Describe("completed pod", func() {
		It("should annotate and delete PVC of matching provisioner when auto deletion is enabled", func() {
			// TODO: configure fake provisioner within reconciler
			pvcMatching := createPVC("matching-pvc", fakeProvisionerDeleteOnPodTermination)
			pvcOther := createPVC("other-pvc", "other."+fakeProvisionerDeleteOnPodTermination)
			podCompleted := createPod("completed-matching-provisioner", []string{"true"}, corev1.RestartPolicyNever, pvcOther, pvcMatching)
			setPodPhase(podCompleted, corev1.PodSucceeded)
			verify(pvcMatching, hasBeenDeleted(pvcMatching))
		})
		It("should not annotate or delete PVC of not matching provisioner", func() {
			pvcMatching := createPVC("matching-provisioner-pvc", fakeProvisionerDeleteOnPodTermination)
			pvcOther := createPVC("other-provisioner-pvc", "other."+fakeProvisionerDeleteOnPodTermination)
			podCompleted := createPod("completed-other-provisioner", []string{"true"}, corev1.RestartPolicyNever, pvcOther, pvcMatching)
			setPodPhase(podCompleted, corev1.PodSucceeded)
			verify(pvcOther, notAfter(5*time.Second, hasBeenDeleted(pvcOther)))
		})
		It("should not annotate or delete PVC of matching provisioner when auto deletion is disabled", func() {
			pvcMatching := createPVC("matching-provisioner-deletion-disabled-pvc", fakeProvisionerNoDeletion)
			pvcOther := createPVC("other-provisionerx-pvc", "other."+fakeProvisionerNoDeletion)
			podCompleted := createPod("completed-other-provisionerx", []string{"true"}, corev1.RestartPolicyNever, pvcOther, pvcMatching)
			setPodPhase(podCompleted, corev1.PodSucceeded)
			verify(pvcMatching, notAfter(5*time.Second, hasBeenDeleted(pvcMatching)))
		})
	})
	Describe("running pod", func() {
		It("should not annotate or delete PVC of running Pod", func() {
			pvcMatchingActive := createPVC("matching-active-pvc", fakeProvisionerDeleteOnPodTermination)
			podRunning := createPod("running", []string{"/bin/sleep", "10000"}, corev1.RestartPolicyNever, pvcMatchingActive)
			setPodPhase(podRunning, corev1.PodRunning)
			verify(pvcMatchingActive, notAfter(5*time.Second, hasBeenDeleted(pvcMatchingActive)))
		})
	})
	Describe("restartable pod", func() {
		It("should not annotate or delete PVC of restartable Pod", func() {
			pvcRestarting := createPVC("restarting-pvc", fakeProvisionerDeleteOnPodTermination)
			podRestarting := createPod("restarting", []string{"true"}, corev1.RestartPolicyAlways, pvcRestarting)
			setPodPhase(podRestarting, corev1.PodSucceeded)
			verify(pvcRestarting, notAfter(5*time.Second, hasBeenDeleted(pvcRestarting)))
		})
	})
})

func createPod(name string, args []string, restartPolicy corev1.RestartPolicy, pvcs ...*corev1.PersistentVolumeClaim) *corev1.Pod {
	pod := &corev1.Pod{}
	pod.Name = name
	pod.Namespace = testNamespace
	pod.Annotations = map[string]string{storageapi.AnnotationPersistentVolumeClaimNoProtection: storageapi.Enabled}
	pod.Spec.RestartPolicy = restartPolicy
	c := corev1.Container{
		Name:  "fakecontainer",
		Image: "alpine:3.12",
		Args:  args,
	}
	for _, pvc := range pvcs {
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name: pvc.Name,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: pvc.Name,
					ReadOnly:  false,
				},
			},
		})
		c.VolumeMounts = append(c.VolumeMounts, corev1.VolumeMount{
			Name:      pvc.Name,
			MountPath: "/" + pvc.Name,
		})
	}
	pod.Spec.Containers = []corev1.Container{c}
	err := k8sClient.Create(context.TODO(), pod)
	Expect(err).ShouldNot(HaveOccurred())
	return pod
}

func setPodPhase(pod *corev1.Pod, phase corev1.PodPhase) {
	Eventually(func() (err error) {
		defer GinkgoRecover()
		if pod.Status.Phase != phase {
			pod.Status.Phase = phase
			err = k8sClient.Status().Update(context.TODO(), pod)
		}
		return err
	}, "15s", "1s").ShouldNot(HaveOccurred())
}
