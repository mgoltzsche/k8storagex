# cache-provisioner

A simple, fast and scalable cache for distributed (build) jobs (early development).  

Manages distributed, layered, Copy-On-Write cache file systems as containers and provisions them as `PersistentVolumes` (PV) in Kubernetes.
[rancher/local-path-provisioner](https://github.com/rancher/local-path-provisioner) is configured with a custom helper Pod to set up and commit a [buildah](https://github.com/containers/buildah) container as `hostpath` PV (soon also synchronizing with a docker registry).

## Motivation

Build tools that require a lot of dynamic dependencies like Docker/Podman or
Maven don't perform well without its cache backed by persistent storage.
When distributed builds are set up without such a persistent cache
they are often slower than locally run builds which can impact development
velocity.  

Tools like Kaniko or Makisu that already support distributed caching are still
slower than local builds since they need to synchronize/transfer the cache
during the build which increases the build duration while still not guaranteeing
full cache consistency since the last cache writer wins (which is acceptable though).  

As a simple solution a single PVC per build could be used but this reduces build concurrency and availability.
Alternatively a `hostpath` Volume could be configured per project but that requires privileges,
is not synchronized between nodes, cannot be size limited and may cause problems
when written by multiple jobs concurrently.  

Since job cache storage is a general problem that applies to most build
pipelines in general it should be solved as an infrastructure problem rather
than that of a particular application or pipeline.
Doing so also allows to reduce the time cache synchronizations are blocking builds.

## Idea

A `PersistentVolume` should be requested for a particular cache name with accessMode `ReadWriteOnce`.
A cache name is unique per Kubernetes `Namespace`.
When a new `PersistentVolume` is created it is initialized with the contents
of the cache identified by the given name.
When a `PersistentVolume` is deleted (and it is the master for its cache name on that node)
its contents are written back to the cache name from where the next `PersistentVolume`
that is requested afterwards for the same cache name is initialized.
However during that process each `PersistentVolume` remains both writeable
and isolated with the help of overlayfs using the shared cache as lower layer
and a separate dir per `PersistentVolume` as upper layer (like docker's image layer fs).  

The cache name as well as eligibility to write the master cache could be
specified as PVC annotations. Distinction between builds that can read and
builds that can write the shared cache is necessary at least in OpenSource
projects where a PR build could inject malware into regular releases
by manipulating the shared cache.  

Though the shared cache is only committed when a `PersistentVolume` is deleted
which therefore must happen directly after each build but PVC/PV deletion is
blocked by the `kubernetes.io/pvc-protection` finalizer as long as the build
pod refers to it which is the case in e.g. a
[Tekton Pipeline](https://github.com/tektoncd/pipeline/blob/v0.15.1/docs/pipelineruns.md#specifying-resources).
However an additional controller could be written that watches Pods and,
on Pod termination (if Pod's `restartPolicy: Never`), deletes the associated PVC,
waits for it to be in `Terminating` state and remove the finalizer
so that the corresponding PV gets committed/deleted.


## Development

### Generate code/manifests

```sh
make generate manifests static-manifests
```

### Build
Build binaries:
```sh
make kube-cache manager
```

### Test
Test kube-cache binary:
```sh
make test-kube-cache
```

### Load images into kind cluster
In order to test this component locally the images can be built and loaded into kind:
```sh
make kind-load-images
```

### Deploy
The default configuration is known to work with [kind](https://github.com/kubernetes-sigs/kind) (`kind create cluster`) and [minikube](https://github.com/kubernetes/minikube) (`minikube start`) but should work with other clusters as well.  

Deploy to a Kubernetes cluster (using [kpt](https://github.com/GoogleContainerTools/kpt)):
```sh
make deploy
```
Undeploy:
```sh
make undeploy
```
