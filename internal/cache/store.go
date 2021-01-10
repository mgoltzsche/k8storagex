package cache

import (
	"context"
	"os"
	"path/filepath"

	"github.com/containers/buildah"
	"github.com/containers/buildah/define"
	"github.com/containers/buildah/util"
	imgstorage "github.com/containers/image/v5/storage"
	"github.com/containers/image/v5/types"
	"github.com/containers/storage"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// see https://github.com/containers/buildah/blob/master/docs/tutorials/04-include-in-your-build-tool.md

type CacheMountOptions struct {
	Context       context.Context
	ContainerName string
	ExtMountDir   string
	Image         string
}

func (o *CacheMountOptions) validate() error {
	if dir := o.ExtMountDir; dir != "" {
		if !filepath.IsAbs(dir) {
			return errors.Errorf("non-absolute mount path %q provided", dir)
		}
	}
	return nil
}

func (o *CacheMountOptions) containerName() (string, error) {
	if err := o.validate(); err != nil {
		return "", err
	}
	if o.ContainerName != "" {
		return o.ContainerName, nil
	}
	if o.ExtMountDir != "" {
		name := filepath.Base(o.ExtMountDir)
		if name == "" || name == ".." || name == "." || name == "/" {
			return "", errors.Errorf("cannot derive container name from provided mount path %q, requires sub directory", o.ExtMountDir)
		}
		return filepath.Clean(o.ExtMountDir), nil
	}
	return "", nil
}

// Store represents the cache store.
// Un/Mount must be run as root and requires storage.conf to configure kernel space overlayfs (and mount option "nodev").
type Store struct {
	store storage.Store
}

// New creates a new cache store
func New(store storage.Store) *Store {
	return &Store{store: store}
}

func (s *Store) Free() {
	s.store.Free()
}

func (s *Store) Mount(opts CacheMountOptions) (dir string, err error) {
	name, err := opts.containerName()
	if err != nil {
		return "", err
	}
	imgRef, err := imgstorage.Transport.ParseStoreReference(s.store, opts.Image)
	if err != nil {
		return "", errors.Wrap(err, "invalid image name provided")
	}
	opts.Image = imgRef.DockerReference().String()
	if opts.ExtMountDir != "" {
		if err = os.Mkdir(opts.ExtMountDir, 0000); err != nil {
			return "", err
		}
		defer func() {
			if err != nil {
				_ = os.Remove(opts.ExtMountDir)
			}
		}()
	}
	systemContext := &types.SystemContext{}
	_, img, err := util.FindImage(s.store, "", systemContext, opts.Image)
	if err != nil {
		if errors.Cause(err) != storage.ErrImageUnknown {
			return "", err
		}
		logrus.WithField("image", opts.Image).Info("Creating cache from scratch since image does not exist")
		opts.Image = "scratch"
	} else {
		logrus.
			WithField("image", opts.Image).
			WithField("imageID", img.ID).
			Info("Creating cache container from image")
		opts.Image = img.ID
	}
	builderOpts := buildah.BuilderOptions{
		Container:        name,
		FromImage:        opts.Image,
		PullPolicy:       define.PullNever,
		Isolation:        buildah.IsolationChroot,
		CommonBuildOpts:  &buildah.CommonBuildOptions{},
		ConfigureNetwork: buildah.NetworkDisabled,
		SystemContext:    systemContext,
	}
	builder, err := buildah.NewBuilder(opts.Context, s.store, builderOpts)
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = builder.Unmount()
			_ = builder.Delete()
		}
	}()

	dir, err = builder.Mount(builder.MountLabel)
	if err != nil {
		return "", err
	}
	if opts.ExtMountDir != "" {
		builder.Args = map[string]string{"MOUNT_DIR": opts.ExtMountDir}
		if err = builder.Save(); err != nil {
			return "", err
		}
		logrus.Debugf("Mounting container dir %s at %s", dir, opts.ExtMountDir)
		err = mount(dir, opts.ExtMountDir)
		dir = opts.ExtMountDir
	}
	return dir, err
}

func (s *Store) Unmount(opts CacheMountOptions) (imageID string, err error) {
	if opts.ContainerName == "" && opts.ExtMountDir == "" {
		return "", errors.New("neither container name nor mount path provided")
	}
	name, err := opts.containerName()
	if err != nil {
		return "", err
	}
	var imgRef types.ImageReference
	if opts.Image != "" {
		imgRef, err = imgstorage.Transport.ParseStoreReference(s.store, opts.Image)
		if err != nil {
			return "", errors.Wrap(err, "invalid image name provided")
		}
	}
	dir := opts.ExtMountDir
	if dir != "" {
		if e := unmountAndDelete(dir); e != nil && err == nil {
			err = e
		}
	}
	builder, err := buildah.OpenBuilder(s.store, name)
	if err != nil {
		if opts.ContainerName == "" {
			builder, err = buildah.OpenBuilderByPath(s.store, opts.ExtMountDir)
			if err != nil {
				return "", errors.Wrapf(err, "find cache container by path %s", opts.ExtMountDir)
			}
		} else {
			return "", errors.Wrapf(err, "find cache container %q", name)
		}
	}
	if builder.Args != nil {
		dir = builder.Args["MOUNT_DIR"]
		if dir != "" {
			err = unmountAndDelete(dir)
		}
	}
	defer func() {
		if opts.Context.Err() == nil {
			logrus.WithField("container", builder.ContainerID).Debug("Deleting container")
			if e := builder.Delete(); e != nil && err == nil {
				err = e
			}
		}
	}()
	if e := builder.Unmount(); e != nil && err == nil {
		err = e
	}
	if err != nil {
		return "", err
	}
	return s.commit(opts.Context, builder, imgRef)
}

func unmountAndDelete(dir string) error {
	mountLog := logrus.WithField("dir", dir)
	mountLog.Debug("Unmounting cache")
	e := unmount(dir)
	if err := os.Remove(dir); err != nil && !os.IsNotExist(err) {
		if e != nil {
			mountLog.Warn(e)
		}
		return err
	}
	return nil
}

func (s *Store) commit(ctx context.Context, builder *buildah.Builder, imgRef types.ImageReference) (imageID string, err error) {
	c, err := s.store.Container(builder.ContainerID)
	if err != nil {
		return "", err
	}
	changes, err := s.store.Changes("", c.LayerID)
	if err != nil {
		return "", err
	}
	imageID = c.ImageID
	if imgRef != nil {
		if len(changes) == 0 {
			if imageID != "" {
				logrus.Info("Skipping commit since nothing changed")
				return imageID, nil
			}
		}
		imgLog := logrus.WithField("image", imgRef.DockerReference().String())
		for _, ch := range changes {
			imgLog.WithField("path", ch.Path).WithField("kind", ch.Kind).Info("Path changed")
		}
		imageID, _, _, err = builder.Commit(ctx, imgRef, buildah.CommitOptions{})
		if err != nil {
			return "", errors.Wrap(err, "commit")
		}
		logMsg := "Created new image with additional layer"
		if len(changes) == 0 {
			logMsg = "Created new empty image"
		}
		imgLog.WithField("imageID", imageID).Info(logMsg)
	}
	return imageID, nil
}
