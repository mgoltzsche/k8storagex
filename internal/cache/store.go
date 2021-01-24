package cache

import (
	"context"
	"fmt"
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

type Store interface {
	Free()
	Mount(CacheMountOptions) (dir string, err error)
	Unmount(CacheMountOptions) (imageID string, newImage bool, err error)
	Prune(context.Context) error
}

var _ Store = &store{}

type CacheMountOptions struct {
	Context        context.Context
	Image          string
	ContainerName  string
	ExtMountDir    string
	Commit         bool
	CacheName      string
	CacheNamespace string
}

func (o *CacheMountOptions) validate() error {
	if o.Image == "" && o.CacheName == "" {
		return errors.Errorf("neither cache name nor image specified")
	}
	if dir := o.ExtMountDir; dir != "" {
		if !filepath.IsAbs(dir) {
			return errors.Errorf("non-absolute mount path %q provided", dir)
		}
	}
	return nil
}

func (o *CacheMountOptions) containerName() (string, error) {
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

func (o *CacheMountOptions) imageName() string {
	if o.Image == "" {
		ns := o.CacheNamespace
		if ns == "" {
			ns = "default"
		}
		return fmt.Sprintf("local/cache/%s/%s:latest", ns, o.CacheName)
	}
	return o.Image
}

// Store represents the cache store.
// Un/Mount must be run as root and requires storage.conf to configure kernel space overlayfs (and mount option "nodev").
type store struct {
	store storage.Store
	log   *logrus.Entry
}

// New creates a new cache store
func New(s storage.Store, log *logrus.Entry) Store {
	return &store{store: s, log: log}
}

func (s *store) Free() {
	s.store.Free()
}

func (s *store) Mount(opts CacheMountOptions) (dir string, err error) {
	if err = opts.validate(); err != nil {
		return "", err
	}
	name, err := opts.containerName()
	if err != nil {
		return "", err
	}
	imageName := opts.imageName()
	if imageName != "" {
		imgRef, err := imgstorage.Transport.ParseStoreReference(s.store, imageName)
		if err != nil {
			return "", errors.Wrap(err, "invalid image name")
		}
		imageName = imgRef.DockerReference().String()
	}
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
	imgLog := s.log.WithField("image", imageName)
	_, img, err := util.FindImage(s.store, "", systemContext, imageName)
	if err != nil {
		if errors.Cause(err) != storage.ErrImageUnknown {
			return "", err
		}
		imgLog.Info("creating empty cache since image does not exist")
		imageName = "scratch"
	} else {
		imgLog.WithField("imageID", img.ID).Info("creating cache container")
		imageName = img.ID
	}
	builderOpts := buildah.BuilderOptions{
		Container:        name,
		FromImage:        imageName,
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
		s.log.Debugf("mounting container dir %s at %s", dir, opts.ExtMountDir)
		err = mount(dir, opts.ExtMountDir)
		dir = opts.ExtMountDir
	}
	return dir, err
}

func (s *store) Unmount(opts CacheMountOptions) (imageID string, newImage bool, err error) {
	if err = opts.validate(); err != nil {
		return "", false, err
	}
	if opts.ContainerName == "" && opts.ExtMountDir == "" {
		return "", false, errors.New("neither container name nor mount path provided")
	}
	name, err := opts.containerName()
	if err != nil {
		return "", false, err
	}
	imageName := opts.imageName()
	var imgRef types.ImageReference
	if imageName != "" {
		imgRef, err = imgstorage.Transport.ParseStoreReference(s.store, imageName)
		if err != nil {
			return "", false, errors.Wrap(err, "invalid image name provided")
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
				return "", false, errors.Wrapf(err, "find cache container by path %s", opts.ExtMountDir)
			}
		} else {
			return "", false, errors.Wrapf(err, "find cache container %q", name)
		}
	}
	if builder.Args != nil {
		dir = builder.Args["MOUNT_DIR"]
		if dir != "" && dir != opts.ExtMountDir {
			err = unmountAndDelete(dir)
		}
	}
	defer func() {
		if opts.Context.Err() == nil {
			logrus.WithField("container", builder.ContainerID).Debug("deleting container")
			if e := builder.Delete(); e != nil && err == nil {
				err = e
			}
		}
	}()
	if e := builder.Unmount(); e != nil && err == nil {
		err = e
	}
	if err != nil || !opts.Commit {
		return "", false, err
	}
	return s.commit(opts.Context, builder, imgRef)
}

func unmountAndDelete(dir string) error {
	mountLog := logrus.WithField("dir", dir)
	mountLog.Debug("unmounting cache")
	e := unmount(dir)
	if err := os.Remove(dir); err != nil && !os.IsNotExist(err) {
		if e != nil {
			mountLog.Warn(e)
		}
		return err
	}
	return nil
}

func (s *store) commit(ctx context.Context, builder *buildah.Builder, imgRef types.ImageReference) (imageID string, newImage bool, err error) {
	c, err := s.store.Container(builder.ContainerID)
	if err != nil {
		return "", false, err
	}
	changes, err := s.store.Changes("", c.LayerID)
	if err != nil {
		return "", false, err
	}
	imageID = c.ImageID
	if imgRef != nil {
		imgLog := logrus.WithField("image", imgRef.DockerReference().String())
		cLog := imgLog.WithField("container", c.ID)
		if len(c.Names) == 1 {
			cLog = cLog.WithField("volume", c.Names[0])
		}
		if len(changes) == 0 {
			if imageID != "" {
				cLog.Info("skipping commit since nothing changed")
				return imageID, false, nil
			}
		}
		for _, ch := range changes {
			imgLog.WithField("path", ch.Path).WithField("kind", ch.Kind).Info("path changed")
		}
		imageID, _, _, err = builder.Commit(ctx, imgRef, buildah.CommitOptions{})
		if err != nil {
			return "", false, errors.Wrap(err, "commit")
		}
		logMsg := "created new image from volume"
		if len(changes) == 0 {
			logMsg = "created new empty image"
		}
		cLog.WithField("imageID", imageID).Info(logMsg)
	}
	return imageID, true, nil
}
