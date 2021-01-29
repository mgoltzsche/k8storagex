package dcowfs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/containers/buildah"
	//"github.com/containers/buildah/util"
	"github.com/containers/image/v5/docker/reference"

	//"github.com/containers/image/v5/image"
	imgstorage "github.com/containers/image/v5/storage"
	"github.com/containers/image/v5/transports/alltransports"
	"github.com/containers/image/v5/types"
	"github.com/containers/storage"

	//"github.com/docker/distribution/registry/api/errcode"
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

// Store represents the cache store.
// Un/Mount must be run as root and requires storage.conf to configure kernel space overlayfs (and mount option "nodev").
type store struct {
	store         storage.Store
	log           *logrus.Entry
	systemContext types.SystemContext
}

// New creates a new cache store
func New(s storage.Store, systemContext types.SystemContext, log *logrus.Entry) Store {
	return &store{store: s, systemContext: systemContext, log: log}
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
	//imageName := localImageName(&opts)
	imgLog := s.log
	pullPolicy := buildah.PullNever
	if opts.Image != "" {
		imgLog = s.log.WithField("image", opts.Image)
		pullPolicy = buildah.PullAlways
	}
	imageRef, err := s.imageRef(&opts)
	if err != nil {
		return "", err
	}
	imageName := imageRef.DockerReference().String()

	/*if imageName != "" && imageName != "scratch" {
		imgRef, err := alltransports.ParseImageName(imageName)
		if err != nil {
			return "", errors.Wrap(err, "invalid image name")
		}
		localImageName := imgRef.DockerReference().String()
		if opts.Image != "" {
			// pull image if name is specified explicitly or start from scratch
			pullPolicy = buildah.PullAlways
		} else {
			// use local image if exists
			imageName = localImageName
			pullPolicy = buildah.PullNever
			_, _, err = util.FindImage(s.store, "", &s.systemContext, imageName)
			if err != nil {
				if errors.Cause(err) != storage.ErrImageUnknown {
					return "", errors.Wrap(err, "find local cache image")
				}
				imageName = "scratch" // image does not exist locally
			}
		}
	} else {
		imageName = "scratch"
	}*/
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
	/*if imageName == "scratch" {
		imgLog.Info("creating empty cache")
	} else {
		imgLog.Info("creating cache from image")
	}*/
	builder, err := s.newBuilder(opts, name, imageName, pullPolicy)
	if err != nil {
		notFound := strings.HasSuffix(err.Error(), ": manifest unknown") || strings.HasSuffix(err.Error(), " could not be found locally")
		if !notFound || imageName == "scratch" {
			return "", err
		}
		imgLog.Infof("creating empty cache since image %s does not exist", imageName)
		imageName = "scratch"
		pullPolicy = buildah.PullNever
		builder, err = s.newBuilder(opts, name, imageName, pullPolicy)
		if err != nil {
			return "", err
		}
	}
	defer func() {
		if err != nil {
			_ = builder.Unmount()
			_ = builder.Delete()
		}
	}()

	if builder.FromImageID != "" {
		imgLog = imgLog.WithField("imageID", builder.FromImageID)
	}
	imgLog.Info("Mounting cache container")
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

func (s *store) newBuilder(opts CacheMountOptions, name, imageName string, pullPolicy buildah.PullPolicy) (*buildah.Builder, error) {
	builderOpts := buildah.BuilderOptions{
		Container:        name,
		FromImage:        imageName,
		PullPolicy:       pullPolicy,
		Isolation:        buildah.IsolationChroot,
		CommonBuildOpts:  &buildah.CommonBuildOptions{},
		ConfigureNetwork: buildah.NetworkDisabled,
		SystemContext:    &s.systemContext,
	}
	return buildah.NewBuilder(opts.Context, s.store, builderOpts)
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
	imageName := localImageName(&opts)
	/*imgRef, err := s.imageRef(&opts)
	if err != nil {
		return "", false, err
	}*/
	/*var imgRef types.ImageReference
	if imageName != "" {
		imgRef, err = alltransports.ParseImageName(imageName)
		if err != nil {
			return "", false, errors.Wrap(err, "invalid image name provided")
		}
	}*/
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
	localImgRef, err := s.storeImageRef(imageName)
	if err != nil {
		return "", false, err
	}
	imageID, ref, newImage, err := s.commit(opts.Context, builder, localImgRef)
	if err != nil {
		return "", false, err
	}
	if newImage && opts.Image != "" {
		// push image to registry
		err = s.pushImage(opts.Context, ref, imageID, localImgRef)
		if err != nil {
			return "", false, err
		}
	}
	return imageID, newImage, nil
}

func (s *store) storeImageRef(name string) (types.ImageReference, error) {
	return imgstorage.Transport.ParseStoreReference(s.store, name)
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

func (s *store) commit(ctx context.Context, builder *buildah.Builder, imgRef types.ImageReference) (imageID string, ref reference.Canonical, newImage bool, err error) {
	c, err := s.store.Container(builder.ContainerID)
	if err != nil {
		return "", nil, false, err
	}
	changes, err := s.store.Changes("", c.LayerID)
	if err != nil {
		return "", nil, false, err
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
				return imageID, nil, false, nil
			}
		}
		for _, ch := range changes {
			imgLog.WithField("path", ch.Path).WithField("kind", ch.Kind).Info("path changed")
		}

		imageID, ref, _, err = builder.Commit(ctx, imgRef, buildah.CommitOptions{})
		if err != nil {
			return "", nil, false, errors.Wrap(err, "commit")
		}
		logMsg := "created new image from volume"
		if len(changes) == 0 {
			logMsg = "created new empty image"
		}
		cLog.WithField("imageID", imageID).Info(logMsg)
		return imageID, ref, true, nil
	}
	return imageID, nil, false, nil
}

func localImageName(o *CacheMountOptions) string {
	return fmt.Sprintf("fs/%s/%s:latest", o.CacheNamespace, o.CacheName)
}

func (s *store) imageRef(o *CacheMountOptions) (imgRef types.ImageReference, err error) {
	if o.Image == "" {
		if o.CacheName == "" || o.CacheNamespace == "" {
			return nil, fmt.Errorf("cache name and namespace must be specified")
		}
		localName := localImageName(o)
		return imgstorage.Transport.ParseStoreReference(s.store, localName)
	}
	imgRef, err = alltransports.ParseImageName(o.Image)
	err = errors.Wrap(err, "invalid image name provided")
	return
}
