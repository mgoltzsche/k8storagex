/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
	"time"

	storageapi "github.com/mgoltzsche/k8storagex/api/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg                   *rest.Config
	k8sClient             client.Client
	testeeCancelFunc      context.CancelFunc
	testEnv               *envtest.Environment
	testNamespace         string
	testNamespaceResource = &corev1.Namespace{}
	testProvisioners      = newProvisioners()
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"Controller Suite",
		[]Reporter{printer.NewlineReporter{}})
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "config", "crd", "bases")},
	}

	err := downloadKubebuilderAssetsIfNotExist(filepath.Join("..", "..", "build", "kubebuilder"))
	Expect(err).ShouldNot(HaveOccurred())

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = storageapi.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	// Set up controller manager
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:             scheme.Scheme,
		LeaderElection:     false,
		MetricsBindAddress: "127.0.0.1:0",
	})
	Expect(err).ToNot(HaveOccurred())
	Expect(mgr).ToNot(BeNil())
	ctx, cancel := context.WithCancel(context.Background())
	testeeCancelFunc = cancel
	go func() {
		defer GinkgoRecover()
		err = mgr.Start(ctx)
		Expect(err).ToNot(HaveOccurred())
	}()

	// Set up test Namespace and StorageClass
	Eventually(func() error {
		testNamespace = fmt.Sprintf("test-namespace-%d", rand.Int63())
		testNamespaceResource.Name = testNamespace
		return k8sClient.Create(context.TODO(), testNamespaceResource)
	}, "10s", "1s").ShouldNot(HaveOccurred())

	// Set up controllers
	err = (&PodReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controllers").WithName("Pod"),
		Scheme:       mgr.GetScheme(),
		Provisioners: testProvisioners,
	}).SetupWithManager(mgr)
	Expect(err).ToNot(HaveOccurred())

}, 60)

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	testeeCancelFunc()
	k8sClient.Delete(context.TODO(), testNamespaceResource)
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

func downloadKubebuilderAssetsIfNotExist(destDir string) error {
	if os.Getenv("KUBEBUILDER_ASSETS") != "" {
		fmt.Println("Skipping kubebuilder assets download since KUBEBUILDER_ASSETS env var is specified")
		return nil
	}
	destDir, err := filepath.Abs(destDir)
	if err != nil {
		return err
	}
	kubebuilderVersion := "2.3.1"
	kubebuilderSubDir := fmt.Sprintf("kubebuilder_%s_%s_%s", kubebuilderVersion, goruntime.GOOS, goruntime.GOARCH)
	kubebuilderBinDir := filepath.Join(destDir, kubebuilderSubDir, "bin")
	err = os.Setenv("KUBEBUILDER_ASSETS", kubebuilderBinDir)
	if err != nil {
		return err
	}
	if _, err = os.Stat(kubebuilderBinDir); err == nil {
		fmt.Println("Using kubebuilder assets at", kubebuilderBinDir)
		return nil // already downloaded
	}
	fmt.Println("Downloading kubebuilder assets to", destDir)
	kubebuilderTarGzURL := fmt.Sprintf("https://go.kubebuilder.io/dl/%s/%s/%s", kubebuilderVersion, goruntime.GOOS, goruntime.GOARCH)
	resp, err := http.Get(kubebuilderTarGzURL) // #nosec
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	destParentDir := filepath.Dir(destDir)
	err = os.MkdirAll(destParentDir, 0750)
	if err != nil {
		return err
	}
	tmpDir, err := ioutil.TempDir(destParentDir, ".tmp-kubebuilder-assets-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	tarStream, err := gzip.NewReader(resp.Body)
	if err != nil {
		return err
	}
	tarReader := tar.NewReader(tarStream)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		destFile := filepath.Join(tmpDir, header.Name) // #nosec
		switch header.Typeflag {
		case tar.TypeDir:
			if err = os.Mkdir(destFile, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			err = os.MkdirAll(filepath.Dir(destFile), 0755)
			if err != nil {
				return err
			}
			f, err := os.OpenFile(destFile, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0755)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.Copy(f, tarReader) // #nosec
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("extract kubebuilder tar: entry %s has unknown type %d", header.Name, header.Typeflag)
		}
	}
	err = os.RemoveAll(destDir)
	if err != nil {
		return err
	}
	return os.Rename(tmpDir, destDir)
}

func verify(o client.Object, fn func(error) error) {
	Eventually(func() error {
		defer GinkgoRecover()
		name := types.NamespacedName{Name: o.GetName(), Namespace: o.GetNamespace()}
		return fn(k8sClient.Get(context.TODO(), name, o))
	}, "10s", "1s").ShouldNot(HaveOccurred())
}

func notAfter(delay time.Duration, fn func(error) error) func(error) error {
	time.Sleep(delay)
	return func(err error) error {
		err = fn(err)
		if err == nil {
			return fmt.Errorf("expected error to be returned")
		}
		return nil
	}
}

func hasBeenDeleted(pvc *corev1.PersistentVolumeClaim) func(error) error {
	return func(err error) error {
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}
		if pvc.Annotations[storageapi.AnnotationPersistentVolumeClaimNoProtection] != storageapi.Enabled {
			err = fmt.Errorf("PVC has not been annotated. %s", err)
		}
		if pvc.GetDeletionTimestamp() == nil {
			err = fmt.Errorf("PVC has no deletion timestamp")
		}
		return err
	}
}

func createPVC(name string, provisionerName string) *corev1.PersistentVolumeClaim {
	pvc := &corev1.PersistentVolumeClaim{}
	pvc.Name = name
	pvc.Namespace = testNamespace
	pvc.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	pvc.Spec.Resources.Requests = map[corev1.ResourceName]resource.Quantity{corev1.ResourceStorage: resource.MustParse("1G")}
	pvc.Finalizers = []string{finalizerPVCProtection}
	pvc.Annotations = map[string]string{"volume.beta.kubernetes.io/storage-provisioner": provisionerName}
	err := k8sClient.Create(context.TODO(), pvc)
	Expect(err).ShouldNot(HaveOccurred())
	return pvc
}
