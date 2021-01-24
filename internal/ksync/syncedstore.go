package ksync

import (
	"fmt"

	"github.com/mgoltzsche/cache-provisioner/internal/cache"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type synchronizedStore struct {
	cache.Store
	cluster  *Updater
	nodeName string
}

func Synchronized(store cache.Store, client client.Client, nodeName string) cache.Store {
	return &synchronizedStore{
		Store:    store,
		cluster:  &Updater{client: client},
		nodeName: nodeName,
	}
}

func (s *synchronizedStore) Mount(opts cache.CacheMountOptions) (dir string, err error) {
	// TODO: change CLI to provide cache name instead of image
	if opts.CacheName == "" || opts.CacheNamespace == "" {
		return "", fmt.Errorf("no cache name or namespace provided")
	}
	cacheName := types.NamespacedName{Name: opts.CacheName, Namespace: opts.CacheNamespace}
	opts.Image, err = s.cluster.RegisterCacheVolume(opts.Context, cacheName, s.nodeName, opts.ContainerName, opts.Image, true)
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			s.cluster.UnregisterCacheVolume(opts.Context, cacheName, s.nodeName, opts.ContainerName, err)
		}
	}()
	return s.Store.Mount(opts)
}

func (s *synchronizedStore) Unmount(opts cache.CacheMountOptions) (imageID string, newImage bool, err error) {
	if opts.CacheName == "" || opts.CacheNamespace == "" {
		return "", false, fmt.Errorf("no cache name or namespace provided")
	}
	cacheName := types.NamespacedName{Name: opts.CacheName, Namespace: opts.CacheNamespace}
	commit, syncErr := s.cluster.PrepareCommit(opts.Context, cacheName, s.nodeName, opts.ContainerName)
	defer func() {
		if err != nil {
			err = fmt.Errorf("unmount: %w", err)
		}
		err = s.cluster.UnregisterCacheVolume(opts.Context, cacheName, s.nodeName, opts.ContainerName, err)
	}()
	opts.Commit = commit
	if !commit {
		opts.Image = ""
	}
	imageID, newImage, err = s.Store.Unmount(opts)
	if err == nil {
		err = syncErr
	}
	return
}
