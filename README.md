# Extensible Kubernetes storage provisioners

A host directory and cache provisioner for Kubernetes.  

It provides a simple, fast and scalable caching solution for distributed (build) jobs using containers as layered, Copy-on-Write cache file systems that can be provisioned as `PersistentVolumes`.  

Formerly known as "cache-provisioner".

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
make layerfs manager
```

### Test
Test layerfs binary:
```sh
make test-layerfs
```

### Deploy

Deploy to a local Kubernetes cluster (using [kpt](https://github.com/GoogleContainerTools/kpt): `kpt live apply config/static/*`):
```sh
make deploy-$TARGET
```
Undeploy:
```sh
make undeploy-$TARGET
```
where `$TARGET` can be one of:
* `default` - manager and provisioners without registry, pulling public images
* `registry` - manager, provisioners and registry, pulling public images
* `minikube` - like `registry` but builds and deploys the local changes to minikube
* `kind` - like `registry` but builds and deploys the local changes to kind

## Example

Deploy a Pod with a PersistentVolumeClaim that points to a cache named `example-project`:
```sh
kubectl apply -f e2e/test-pod.yaml
```
Watch the Pod being created and how it fetches and runs a podman image:
```sh
$ kubectl logs -f cached-build
Trying to pull docker.io/library/alpine:3.12...
Getting image source signatures
Copying blob sha256:801bfaa63ef2094d770c809815b9e2b9c1194728e5e754ef7bc764030e140cea
Copying config sha256:389fef7118515c70fd6c0e0d50bb75669942ea722ccb976507d7b087e54d5a23
Writing manifest to image destination
Storing signatures
hello from nested container
```

After the Pod terminated the PVC is removed and a `Cache` resource is created that refers to the node the Pod ran on:
```sh
$ kubectl get cache example-project
NAME              AGE
example-project   7s
```

When another Pod is deployed that points to the same cache its volume has the same contents as it had when the last Pod that was using the cache terminated.
For the sake of the example let's delete and recreate the previously applied Pod and PersistentVolumeClaim:
```sh
$ kubectl delete -f e2e/test-pod.yaml
$ kubectl apply -f e2e/test-pod.yaml
```

Although the new Pod has a new PersistentVolumeClaim and PersistentVolume the image is cached and doesn't need to be pulled again:
```sh
$ kubectl logs -f cached-build
hello from nested container
```
