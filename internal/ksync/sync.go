package ksync

import (
	"context"
	"fmt"
	"time"

	cacheapi "github.com/mgoltzsche/cache-provisioner/api/v1alpha1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	kerr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	backoff = wait.Backoff{
		Duration: time.Second,
		Factor:   1,
		Steps:    10,
	}
)

type Updater struct {
	client client.Client
}

func (u *Updater) RegisterCacheVolume(ctx context.Context, cacheName types.NamespacedName, nodeName, volumeName, defaultImage string, commit bool) (image string, err error) {
	err = u.updateCache(ctx, cacheName, true, func(c *cacheapi.Cache) {
		image = c.Status.Image
		if image == "" {
			image = defaultImage
			c.Status.Image = defaultImage
		}
		c.Status.Used++
		addVolume(c, nodeName, volumeName, commit)
	})
	return image, errors.Wrap(err, "register volume in cluster")
}

func (u *Updater) PrepareCommit(ctx context.Context, cacheName types.NamespacedName, nodeName, volumeName string) (commit bool, err error) {
	err = u.updateCache(ctx, cacheName, false, func(c *cacheapi.Cache) {
		if c.Spec.ReadOnly || c.Status.Phase == cacheapi.CachePhaseReject {
			commit = false
		} else {
			vol, err := findVolume(c, nodeName, volumeName)
			if err != nil {
				logrus.WithField("cache", cacheName.String()).
					WithField("node", nodeName).
					WithField("volume", volumeName).
					Warn(err.Error())
				commit = false
				return
			}
			commit = vol.Committable && !commitInProgress(c)
			if commit {
				vol.CommitStartTime = &metav1.Time{Time: time.Now()}
			}
		}
	})
	if kerr.IsNotFound(err) {
		logrus.Warnf("prepare cache commit: cache %s not found", cacheName.String())
		commit = false
		err = nil
	}
	return commit, errors.Wrap(err, "prepare cache commit")
}

func (u *Updater) UnregisterCacheVolume(ctx context.Context, cacheName types.NamespacedName, nodeName, volumeName string, commitErr error) error {
	err := u.updateCache(ctx, cacheName, false, func(c *cacheapi.Cache) {
		node, cacheGeneration := removeVolume(&c.Status, nodeName, volumeName)
		if commitErr != nil {
			node.LastError = &cacheapi.VolumeError{
				VolumeName:      volumeName,
				CacheGeneration: cacheGeneration,
				Error:           commitErr.Error(),
				Happened:        metav1.Time{Time: time.Now()},
			}
		} else {
			node.LastError = nil
		}
	})
	if kerr.IsNotFound(err) {
		logrus.WithField("cache", cacheName.String()).
			WithField("node", nodeName).
			WithField("volume", volumeName).
			Warn("unregister volume from cluster: cache not found")
		err = nil
	}
	return errors.Wrapf(err, "unregister volume %q from cluster", volumeName)
}

func (u *Updater) updateCache(ctx context.Context, name types.NamespacedName, create bool, modify func(*cacheapi.Cache)) error {
	return retry.RetryOnConflict(backoff, func() error {
		var cache cacheapi.Cache
		err := u.client.Get(ctx, name, &cache)
		if err != nil {
			if !kerr.IsNotFound(err) || !create {
				return err
			}
			cache.Name = name.Name
			cache.Namespace = name.Namespace
			err = u.client.Create(ctx, &cache)
			if err != nil && !kerr.IsAlreadyExists(err) {
				return err
			}
			err = u.client.Get(ctx, name, &cache)
			if err != nil {
				return err
			}
		}
		modify(&cache)
		return u.client.Status().Update(ctx, &cache)
	})
}

func addVolume(c *cacheapi.Cache, nodeName, volName string, commit bool) error {
	n := upsertNode(&c.Status, nodeName)
	for _, v := range n.Volumes {
		if v.Name == volName {
			return errors.Errorf("volume %q on node %q already exists", volName, nodeName)
		}
	}
	commit = commit && !c.Spec.ReadOnly
	if commit {
		c.Status.CacheGeneration++
	}
	n.Volumes = append(n.Volumes, cacheapi.VolumeStatus{
		Name:            volName,
		Created:         metav1.Time{Time: time.Now()},
		CacheGeneration: c.Status.CacheGeneration,
		Committable:     commit,
	})
	return nil
}

func removeVolume(cs *cacheapi.CacheStatus, nodeName, volName string) (n *cacheapi.NodeStatus, cacheGeneration *int64) {
	n = upsertNode(cs, nodeName)
	vols := make([]cacheapi.VolumeStatus, 0, len(n.Volumes))
	for _, v := range n.Volumes {
		if v.Name != volName {
			vols = append(vols, v)
			cacheGeneration = &v.CacheGeneration
		}
	}
	n.Volumes = vols
	return
}

func upsertNode(cs *cacheapi.CacheStatus, nodeName string) *cacheapi.NodeStatus {
	for i, n := range cs.Nodes {
		if n.Name == nodeName {
			node := &cs.Nodes[i]
			node.LastUsed = metav1.Time{Time: time.Now()}
			return node
		}
	}
	cs.Nodes = append(cs.Nodes, cacheapi.NodeStatus{
		Name:     nodeName,
		LastUsed: metav1.Time{Time: time.Now()},
	})
	return &cs.Nodes[len(cs.Nodes)-1]
}

func findVolume(c *cacheapi.Cache, nodeName, volumeName string) (*cacheapi.VolumeStatus, error) {
	for _, n := range c.Status.Nodes {
		if n.Name == nodeName {
			for i, v := range n.Volumes {
				if v.Name == volumeName {
					return &n.Volumes[i], nil
				}
			}
			return nil, errors.Errorf("node %q not found in %T %s/%s", nodeName, *c, c.Namespace, c.Name)
		}
	}
	return nil, errors.Errorf("node %q not found in %T %s/%s", nodeName, *c, c.Namespace, c.Name)
}

var commitTimeout = 15 * time.Minute

func commitInProgress(c *cacheapi.Cache) bool {
	for i := range c.Status.Nodes {
		n := &c.Status.Nodes[i]
		for j := range n.Volumes {
			v := &n.Volumes[j]
			if v.CommitStartTime == nil || v.CommitStartTime.Time.Add(commitTimeout).Before(time.Now()) {
				if v.CommitStartTime != nil {
					logrus.WithField("cache", fmt.Sprintf("%s/%s", c.Namespace, c.Name)).
						WithField("node", n.Name).
						WithField("volume", v.Name).
						Errorf("commit timed out after %s - removing its lock from cluster", commitTimeout)
					n.LastError = &cacheapi.VolumeError{
						VolumeName:      v.Name,
						CacheGeneration: &v.CacheGeneration,
						Error:           "commit timed out",
						Happened:        metav1.Time{Time: time.Now()},
					}
					v.CommitStartTime = nil
					removeVolume(&c.Status, n.Name, v.Name)
				}
				return true
			}
		}
	}
	return false
}
