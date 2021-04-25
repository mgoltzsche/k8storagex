package layerfs

import (
	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

// see https://github.com/containers/storage/blob/master/drivers/overlay/mount.go#L34
// and https://github.com/containers/storage/blob/0c5746d37106aabe5b8c9c4ca430e5f15280eee5/drivers/overlay/overlay.go#L432

func mount(srcDir, destDir string) error {
	err := unix.Mount(srcDir, destDir, "", unix.MS_BIND|unix.MS_REC|unix.MS_SHARED, "")
	return errors.Wrapf(err, "bind mount %s to %s", srcDir, destDir)
}

func unmount(destDir string) error {
	err := unix.Unmount(destDir, unix.MNT_DETACH|unix.MNT_FORCE)
	return errors.Wrapf(err, "unmount %s", destDir)
}
