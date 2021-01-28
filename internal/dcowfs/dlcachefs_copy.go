package dcowfs

/*
import (
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/containers/storage"
)

type Store struct {
	store storage.Store
}

func New(opts storage.StoreOptions) (*Store, error) {
	logrus.Debugf("Root: %s", opts.GraphRoot)
	logrus.Debugf("Run Root: %s", opts.RunRoot)
	logrus.Debugf("Driver Name: %s", opts.GraphDriverName)
	logrus.Debugf("Driver Options: %s", opts.GraphDriverOptions)
	store, err := storage.GetStore(opts)
	if err != nil {
		return nil, err
	}
	return &Store{store: store}, nil
}

func (s *Store) Free() {
	s.store.Free()
}

func (s *Store) Mount(name, srcImage, destDir string) (dir string, err error) {
	img, err := s.store.Image(srcImage)
	if errors.Cause(err) == storage.ErrImageUnknown {
		// TODO: eventually at least a layer must be provided
		img, err = s.store.CreateImage("", nil, "", "", &storage.ImageOptions{})
	}
	if err != nil {
		return "", err
	}
	c, err := s.store.CreateContainer("", []string{name}, img.ID, img.TopLayer, "", nil)
	if err != nil {
		return "", err
	}
	dir, err = s.store.Mount(c.ID, "")
	if err != nil {
		return "", err
	}

	return dir, nil
}

func (s *Store) Unmount(name, srcImage, destDir string) (err error) {
	c, err := s.store.Container(name)
	if err != nil {
		return err
	}
	defer func() {
		if e := s.store.Delete(c.ID); e != nil && err == nil {
			err = e
		}
	}()
	mounted, err := s.store.Unmount(c.ID, true)
	if err != nil {
		return err
	}
	if mounted {
		return fmt.Errorf("unmount %s: layer is still mounted", c.ID)
	}
	//img, err := s.store.Image(c.ImageID)
	//if err != nil {
	//	return err
	//}
	changes, err := s.store.Changes("", c.LayerID)
	if err != nil {
		return fmt.Errorf("changes: %w", err)
	}
	for _, c := range changes {
		logrus.WithField("path", c.Path).WithField("kind", c.Kind).Debugln("Path changed")
	}
	if len(changes) == 0 {
		return err
	}
	return err
}

func (s *Store) commit(name, destImage string) error {
	c, err := s.store.Container(name)
	if err != nil {
		return err
	}
	s.store.Changes(c.ID, c.ImageID)
	// TODO: commit if changed
	return nil
}
*/
