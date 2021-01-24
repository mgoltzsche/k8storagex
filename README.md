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

## Roadmap

* Write additional PVC on pod termination deleting controller

Node local cache synchronization:
* Make `rancher/local-path-provisioner` support privileged helper pod.
* Make `rancher/local-path-provisioner` pass through certain PVC/SC/Volume annotations to helper pod as env vars.
* Support `PersistentVolumeClaim` annotation to specify the cache name.
* Support `PersistentVolumeClaim` annotation to allow publication/reusage as shared cache after volume deletion (security).
* Prepare separate container image to be used as `local-path-provisioner` helper to ship minimal buildah and script without redundant code.

Distributed cache synchronization:
* Make `rancher/local-path-provisioner` support optional configuration of a (docker registry) secret and other values (registry name) that can be provided to the helper pod.
* Mark/lock a single `PersistentVolume` per cache name and node as master for that cache name (for push afterwards).
* On every creation of a (master) `PersistentVolume` pull the cache name's latest image from the docker registry (impacts build duration but only to fetch a cache delta - to avoid that pods could have affinity to a group of nodes and/or be sticky to a particular node using preferred nodeAffinity on a project basis).
* When writing the node-locally shared cache name after master volume deletion
  push the resulting image to the registry
  (doesn't impact build duration since it happens after the pod has been terminated).

Size limits:
* Make max cache size configurable - like for container size limits this is only supported when overlay driver is backed by an xfs/btrfs host file system

Cache garbage collection / clear cache:
- either on volume deletion or per CronJob - TBD

Security: Unique UID/GID management - support for privileged build containers run by unprivileged users:
* In order to support running user-specified build tasks (e.g. podman run) as an unprivileged user within a privileged container
  let the provisioner assign a node-unique UID to each `PersistentVolume` and a cluster-unique GID to each cache name
  to allow build pods to switch to that unique unprivileged user dynamically
  before running the user specified tasks with higher privileges but no access
  to other user's files even when breaking out of the container.
* Support readonly mode based on PVC/PV configuration to specify that cache is not committed after PV deletion.
* Enforce readonly access also in --privileged builds using a single GID per cache and a different UID per pod (TBD, master pod has UID==GID to write).
* Make this behaviour configurable via (`StorageClass`) annotation.
