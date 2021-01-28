## Roadmap

* [done] Write additional PVC on pod termination deleting controller - https://github.com/mgoltzsche/pvc-remover/

Node local cache synchronization:
* [done] Make `rancher/local-path-provisioner` support privileged helper pod.
* [done/PR pending] Make `rancher/local-path-provisioner` pass through certain PVC/SC/Volume annotations to helper pod as env vars. https://github.com/rancher/local-path-provisioner/pull/166 - fork published on dockerhub meanwhile
* [done] Support `PersistentVolumeClaim` annotation to specify the cache name.
* [done] Support `PersistentVolumeClaim` annotation to allow publication/reusage as shared cache after volume deletion (security).
* [done] Prepare separate container image to be used as `local-path-provisioner` helper.

Distributed cache synchronization:
* [done] Make `rancher/local-path-provisioner` support optional configuration of a (docker registry) secret and other values (registry name) that can be provided to the helper pod.
* Mark/lock a single `PersistentVolume` per cache name and node as master for that cache name (for push afterwards).
* [supported but not integrated] On every creation of a (master) `PersistentVolume` pull the cache name's latest image from the docker registry (impacts build duration but only to fetch a cache delta - to avoid that pods could have affinity to a group of nodes and/or be sticky to a particular node using preferred nodeAffinity on a project basis).
* [supported] When writing the node-locally shared cache name after master volume deletion
  push the resulting image to the registry
  (doesn't impact build duration since it happens after the pod has been terminated).

Size limits:
* Make max cache size configurable - like for container size limits this is only supported when overlay driver is backed by an xfs/btrfs host file system

Cache garbage collection / clear cache:
- either on volume deletion or per CronJob - TBD

Security: TBD: Unique UID/GID management? - support for privileged build containers run by unprivileged users:
* In order to support running user-specified build tasks (e.g. podman run) as an unprivileged user within a privileged container
  let the provisioner assign a node-unique UID to each `PersistentVolume` and a cluster-unique GID to each cache name
  to allow build pods to switch to that unique unprivileged user dynamically
  before running the user specified tasks with higher privileges but no access
  to other user's files even when breaking out of the container.
* Support readonly mode based on PVC/PV configuration to specify that cache is not committed after PV deletion.
* Enforce readonly access also in --privileged builds using a single GID per cache and a different UID per pod (TBD, master pod has UID==GID to write).
* Make this behaviour configurable via (`StorageClass`) annotation.
