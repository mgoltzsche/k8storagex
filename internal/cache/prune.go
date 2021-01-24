package cache

import (
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/containers/buildah/util"
	is "github.com/containers/image/v5/storage"
	"github.com/containers/image/v5/transports"
	"github.com/containers/image/v5/transports/alltransports"
	"github.com/containers/image/v5/types"
	"github.com/containers/storage"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func (s *store) Prune(ctx context.Context) error {
	systemContext := &types.SystemContext{}
	imagesToDelete, err := findDanglingImages(s.store)
	if err != nil {
		return err
	}
	return deleteImages(ctx, systemContext, s.store, imagesToDelete, false, false, true)
}

// copied from https://github.com/containers/buildah/blob/d10dbf35450aa2be494cadc9f60cd7e849215b4a/cmd/buildah/rmi.go

func deleteImages(ctx context.Context, systemContext *types.SystemContext, store storage.Store, imagesToDelete []string, removeAll, force, prune bool) error {
	var lastError error
	for _, id := range imagesToDelete {
		image, err := getImage(ctx, systemContext, store, id)
		if err != nil || image == nil {
			if lastError != nil {
				fmt.Fprintln(os.Stderr, lastError)
			}
			if err == nil {
				err = storage.ErrNotAnImage
			}
			lastError = errors.Wrapf(err, "could not get image %q", id)
			continue
		}
		if image.ReadOnly {
			if lastError != nil {
				fmt.Fprintln(os.Stderr, lastError)
			}
			lastError = errors.Wrapf(syscall.EINVAL, "can not remove readonly image %q", id)
			continue
		}
		ctrIDs, err := runningContainers(store, image)
		if err != nil {
			if lastError != nil {
				fmt.Fprintln(os.Stderr, lastError)
			}
			lastError = errors.Wrapf(err, "error getting running containers for image %q", id)
			continue
		}
		if len(ctrIDs) > 0 && len(image.Names) <= 1 {
			if force {
				err = removeContainers(ctrIDs, store)
				if err != nil {
					if lastError != nil {
						fmt.Fprintln(os.Stderr, lastError)
					}
					lastError = errors.Wrapf(err, "error removing containers %v for image %q", ctrIDs, id)
					continue
				}
			} else {
				for _, ctrID := range ctrIDs {
					if lastError != nil {
						fmt.Fprintln(os.Stderr, lastError)
					}
					lastError = errors.Wrapf(storage.ErrImageUsedByContainer, "Could not remove image %q (must force) - container %q is using its reference image", id, ctrID)
				}
				continue
			}
		}
		// If the user supplied an ID, we cannot delete the image if it is referred to by multiple tags
		if strings.HasPrefix(image.ID, id) {
			if len(image.Names) > 1 && !force {
				if lastError != nil {
					fmt.Fprintln(os.Stderr, lastError)
				}
				lastError = errors.Errorf("unable to delete %s (must force) - image is referred to in multiple tags", image.ID)
				continue
			}
			// If it is forced, we have to untag the image so that it can be deleted
			image.Names = image.Names[:0]
		} else {
			name, err2 := untagImage(id, store, image)
			if err2 != nil {
				if lastError != nil {
					fmt.Fprintln(os.Stderr, lastError)
				}
				lastError = errors.Wrapf(err2, "error removing tag %q from image %q", id, image.ID)
				continue
			}
			fmt.Printf("untagged: %s\n", name)

			// Need to fetch the image state again after making changes to it i.e untag
			// because only a copy of the image state is returned
			image1, err := getImage(ctx, systemContext, store, image.ID)
			if err != nil || image1 == nil {
				if lastError != nil {
					fmt.Fprintln(os.Stderr, lastError)
				}
				lastError = errors.Wrapf(err, "error getting image after untag %q", image.ID)
			} else {
				image = image1
			}
		}

		isParent, err := imageIsParent(ctx, systemContext, store, image)
		if err != nil {
			if lastError != nil {
				fmt.Fprintln(os.Stderr, lastError)
			}
			lastError = errors.Wrapf(err, "error determining if the image %q is a parent", image.ID)
			continue
		}
		// If the --all flag is not set and the image has named references or is
		// a parent, do not delete image.
		if len(image.Names) > 0 && !removeAll {
			continue
		}

		if isParent && len(image.Names) == 0 && !removeAll {
			if !prune {
				if lastError != nil {
					fmt.Fprintln(os.Stderr, lastError)
				}
				lastError = errors.Errorf("unable to delete %q (cannot be forced) - image has dependent child images", image.ID)
			}
			continue
		}
		id, err := removeImage(ctx, systemContext, store, image)
		if err != nil {
			if lastError != nil {
				fmt.Fprintln(os.Stderr, lastError)
			}
			lastError = errors.Wrapf(err, "error removing image %q", image.ID)
			continue
		}
		fmt.Printf("%s\n", id)
	}

	return lastError
}

// Returns a list of all dangling images
func findDanglingImages(store storage.Store) ([]string, error) {
	imagesToDelete := []string{}

	images, err := store.Images()
	if err != nil {
		return nil, errors.Wrapf(err, "error reading images")
	}
	for _, image := range images {
		if len(image.Names) == 0 {
			imagesToDelete = append(imagesToDelete, image.ID)
		}
	}

	return imagesToDelete, nil
}

func getImage(ctx context.Context, systemContext *types.SystemContext, store storage.Store, id string) (*storage.Image, error) {
	var ref types.ImageReference
	ref, err := properImageRef(ctx, id)
	if err != nil {
		logrus.Debug(err)
	}
	if ref == nil {
		if ref, err = storageImageRef(systemContext, store, id); err != nil {
			logrus.Debug(err)
		}
	}
	if ref == nil {
		if ref, err = storageImageID(ctx, store, id); err != nil {
			logrus.Debug(err)
		}
	}
	if ref != nil {
		image, err2 := is.Transport.GetStoreImage(store, ref)
		if err2 != nil {
			return nil, errors.Wrapf(err2, "error reading image using reference %q", transports.ImageName(ref))
		}
		return image, nil
	}
	return nil, err
}

func untagImage(imgArg string, store storage.Store, image *storage.Image) (string, error) {
	newNames := []string{}
	removedName := ""
	for _, name := range image.Names {
		if matchesReference(name, imgArg) {
			removedName = name
			continue
		}
		newNames = append(newNames, name)
	}
	if removedName != "" {
		if err := store.SetNames(image.ID, newNames); err != nil {
			return "", errors.Wrapf(err, "error removing name %q from image %q", removedName, image.ID)
		}
	}
	return removedName, nil
}

func removeImage(ctx context.Context, systemContext *types.SystemContext, store storage.Store, image *storage.Image) (string, error) {
	parent, err := getParent(ctx, systemContext, store, image)
	if err != nil {
		return "", err
	}
	if _, err := store.DeleteImage(image.ID, true); err != nil {
		return "", errors.Wrapf(err, "could not remove image %q", image.ID)
	}
	for parent != nil {
		nextParent, err := getParent(ctx, systemContext, store, parent)
		if err != nil {
			return image.ID, errors.Wrapf(err, "unable to get parent from image %q", image.ID)
		}
		isParent, err := imageIsParent(ctx, systemContext, store, parent)
		if err != nil {
			return image.ID, errors.Wrapf(err, "unable to get check if image %q is a parent", image.ID)
		}
		// Do not remove if image is a base image and is not untagged, or if
		// the image has more children.
		if len(parent.Names) > 0 || isParent {
			return image.ID, nil
		}
		id := parent.ID
		if _, err := store.DeleteImage(id, true); err != nil {
			logrus.Debugf("unable to remove intermediate image %q: %v", id, err)
		} else {
			fmt.Println(id)
		}
		parent = nextParent
	}
	return image.ID, nil
}

// Returns a list of running containers associated with the given ImageReference
func runningContainers(store storage.Store, image *storage.Image) ([]string, error) {
	ctrIDs := []string{}
	containers, err := store.Containers()
	if err != nil {
		return nil, err
	}
	for _, ctr := range containers {
		if ctr.ImageID == image.ID {
			ctrIDs = append(ctrIDs, ctr.ID)
		}
	}
	return ctrIDs, nil
}

func removeContainers(ctrIDs []string, store storage.Store) error {
	for _, ctrID := range ctrIDs {
		if err := store.DeleteContainer(ctrID); err != nil {
			return errors.Wrapf(err, "could not remove container %q", ctrID)
		}
	}
	return nil
}

// If it's looks like a proper image reference, parse it and check if it
// corresponds to an image that actually exists.
func properImageRef(ctx context.Context, id string) (types.ImageReference, error) {
	var err error
	if ref, err := alltransports.ParseImageName(id); err == nil {
		if img, err2 := ref.NewImageSource(ctx, nil); err2 == nil {
			img.Close()
			return ref, nil
		}
		return nil, errors.Wrapf(err, "error confirming presence of image reference %q", transports.ImageName(ref))
	}
	return nil, errors.Wrapf(err, "error parsing %q as an image reference", id)
}

// If it's looks like an image reference that's relative to our storage, parse
// it and check if it corresponds to an image that actually exists.
func storageImageRef(systemContext *types.SystemContext, store storage.Store, id string) (types.ImageReference, error) {
	ref, _, err := util.FindImage(store, "", systemContext, id)
	if err != nil {
		if ref != nil {
			return nil, errors.Wrapf(err, "error confirming presence of storage image reference %q", transports.ImageName(ref))
		}
		return nil, errors.Wrapf(err, "error confirming presence of storage image name %q", id)
	}
	return ref, err
}

// If it might be an ID that's relative to our storage, truncated or not, so
// parse it and check if it corresponds to an image that we have stored
// locally.
func storageImageID(ctx context.Context, store storage.Store, id string) (types.ImageReference, error) {
	var err error
	imageID := id
	if img, err := store.Image(id); err == nil && img != nil {
		imageID = img.ID
	}
	if ref, err := is.Transport.ParseStoreReference(store, imageID); err == nil {
		if img, err2 := ref.NewImageSource(ctx, nil); err2 == nil {
			img.Close()
			return ref, nil
		}
		return nil, errors.Wrapf(err, "error confirming presence of storage image reference %q", transports.ImageName(ref))
	}
	return nil, errors.Wrapf(err, "error parsing %q as a storage image reference", id)
}

func matchesReference(name, argName string) bool {
	if argName == "" {
		return true
	}
	splitName := strings.Split(name, ":")
	// If the arg contains a tag, we handle it differently than if it does not
	if strings.Contains(argName, ":") {
		splitArg := strings.Split(argName, ":")
		return strings.HasSuffix(splitName[0], splitArg[0]) && (splitName[1] == splitArg[1])
	}
	return strings.HasSuffix(splitName[0], argName)
}

// copied from https://github.com/containers/buildah/blob/d10dbf35450aa2be494cadc9f60cd7e849215b4a/cmd/buildah/common.go#L229

// imageIsParent goes through the layers in the store and checks if i.TopLayer is
// the parent of any other layer in store. Double check that image with that
// layer exists as well.
func imageIsParent(ctx context.Context, sc *types.SystemContext, store storage.Store, image *storage.Image) (bool, error) {
	children, err := getChildren(ctx, sc, store, image, 1)
	if err != nil {
		return false, err
	}
	return len(children) > 0, nil
}

func getImageConfig(ctx context.Context, sc *types.SystemContext, store storage.Store, imageID string) (*imgspecv1.Image, error) {
	ref, err := is.Transport.ParseStoreReference(store, imageID)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to parse reference to image %q", imageID)
	}
	image, err := ref.NewImage(ctx, sc)
	if err != nil {
		if img, err2 := store.Image(imageID); err2 == nil && img.ID == imageID {
			return nil, nil
		}
		return nil, errors.Wrapf(err, "unable to open image %q", imageID)
	}
	config, err := image.OCIConfig(ctx)
	defer image.Close()
	if err != nil {
		return nil, errors.Wrapf(err, "unable to read configuration from image %q", imageID)
	}
	return config, nil
}

func historiesDiffer(a, b []imgspecv1.History) bool {
	if len(a) != len(b) {
		return true
	}
	i := 0
	for i < len(a) {
		if a[i].Created == nil && b[i].Created != nil {
			break
		}
		if a[i].Created != nil && b[i].Created == nil {
			break
		}
		if a[i].Created != nil && b[i].Created != nil && !a[i].Created.Equal(*(b[i].Created)) {
			break
		}
		if a[i].CreatedBy != b[i].CreatedBy {
			break
		}
		if a[i].Author != b[i].Author {
			break
		}
		if a[i].Comment != b[i].Comment {
			break
		}
		if a[i].EmptyLayer != b[i].EmptyLayer {
			break
		}
		i++
	}
	return i != len(a)
}

// getParent returns the image's parent image. Return nil if a parent is not found.
func getParent(ctx context.Context, sc *types.SystemContext, store storage.Store, child *storage.Image) (*storage.Image, error) {
	images, err := store.Images()
	if err != nil {
		return nil, errors.Wrapf(err, "unable to retrieve image list from store")
	}
	var childTopLayer *storage.Layer
	if child.TopLayer != "" {
		childTopLayer, err = store.Layer(child.TopLayer)
		if err != nil {
			return nil, errors.Wrapf(err, "unable to retrieve information about layer %s from store", child.TopLayer)
		}
	}
	childConfig, err := getImageConfig(ctx, sc, store, child.ID)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to read configuration from image %q", child.ID)
	}
	if childConfig == nil {
		return nil, nil
	}
	for _, parent := range images {
		if parent.ID == child.ID {
			continue
		}
		if childTopLayer != nil && parent.TopLayer != childTopLayer.Parent && parent.TopLayer != childTopLayer.ID {
			continue
		}
		parentConfig, err := getImageConfig(ctx, sc, store, parent.ID)
		if err != nil {
			return nil, errors.Wrapf(err, "unable to read configuration from image %q", parent.ID)
		}
		if parentConfig == nil {
			continue
		}
		if len(parentConfig.History)+1 != len(childConfig.History) {
			continue
		}
		if len(parentConfig.RootFS.DiffIDs) > 0 {
			if len(childConfig.RootFS.DiffIDs) < len(parentConfig.RootFS.DiffIDs) {
				continue
			}
			childUsesAllParentLayers := true
			for i := range parentConfig.RootFS.DiffIDs {
				if childConfig.RootFS.DiffIDs[i] != parentConfig.RootFS.DiffIDs[i] {
					childUsesAllParentLayers = false
					break
				}
			}
			if !childUsesAllParentLayers {
				continue
			}
		}
		if historiesDiffer(parentConfig.History, childConfig.History[:len(parentConfig.History)]) {
			continue
		}
		return &parent, nil
	}
	return nil, nil
}

// getChildren returns a list of the imageIDs that depend on the image
func getChildren(ctx context.Context, sc *types.SystemContext, store storage.Store, parent *storage.Image, max int) ([]string, error) {
	var children []string
	images, err := store.Images()
	if err != nil {
		return nil, errors.Wrapf(err, "unable to retrieve images from store")
	}
	parentConfig, err := getImageConfig(ctx, sc, store, parent.ID)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to read configuration from image %q", parent.ID)
	}
	if parentConfig == nil {
		return nil, nil
	}
	for _, child := range images {
		if child.ID == parent.ID {
			continue
		}
		var childTopLayer *storage.Layer
		if child.TopLayer != "" {
			childTopLayer, err = store.Layer(child.TopLayer)
			if err != nil {
				return nil, errors.Wrapf(err, "unable to retrieve information about layer %q from store", child.TopLayer)
			}
			if childTopLayer.Parent != parent.TopLayer && childTopLayer.ID != parent.TopLayer {
				continue
			}
		}
		childConfig, err := getImageConfig(ctx, sc, store, child.ID)
		if err != nil {
			return nil, errors.Wrapf(err, "unable to read configuration from image %q", child.ID)
		}
		if childConfig == nil {
			continue
		}
		if len(parentConfig.History)+1 != len(childConfig.History) {
			continue
		}
		if historiesDiffer(parentConfig.History, childConfig.History[:len(parentConfig.History)]) {
			continue
		}
		children = append(children, child.ID)
		if max > 0 && len(children) >= max {
			break
		}
	}
	return children, nil
}
