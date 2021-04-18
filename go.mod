module github.com/mgoltzsche/cache-provisioner

go 1.14

require (
	github.com/a8m/envsubst v1.2.0
	github.com/containers/buildah v1.19.0
	github.com/containers/common v0.33.0
	github.com/containers/image/v5 v5.9.0
	github.com/containers/storage v1.24.5
	github.com/go-logr/logr v0.3.0
	github.com/kr/pretty v0.2.1 // indirect
	github.com/onsi/ginkgo v1.14.2
	github.com/onsi/gomega v1.10.4
	github.com/opencontainers/image-spec v1.0.2-0.20190823105129-775207bd45b6
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.7.0
	github.com/spf13/cobra v1.1.1
	github.com/stretchr/testify v1.6.1
	golang.org/x/sys v0.0.0-20201201145000-ef89a241ccb3
	k8s.io/api v0.19.2
	k8s.io/apimachinery v0.19.2
	k8s.io/client-go v0.19.2
	sigs.k8s.io/controller-runtime v0.7.0
)
