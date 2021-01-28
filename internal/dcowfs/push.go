package dcowfs

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/containers/common/pkg/retry"
	"github.com/containers/image/v5/copy"
	"github.com/containers/image/v5/docker/reference"
	"github.com/containers/image/v5/signature"
	"github.com/containers/image/v5/storage"
	"github.com/containers/image/v5/types"
	"github.com/pkg/errors"
)

func (s *store) pushImage(ctx context.Context, srcImageRef reference.Named, srcImageID string, destRef types.ImageReference) error {
	policy := &signature.Policy{Default: []signature.PolicyRequirement{signature.NewPRInsecureAcceptAnything()}}
	policyCtx, err := signature.NewPolicyContext(policy)
	if err != nil {
		panic(err)
	}
	srcRef, err := storage.Transport.NewStoreReference(s.store, srcImageRef, srcImageID)
	if err != nil {
		return errors.Wrap(err, "push source")
	}
	s.log.WithField("src", srcRef.StringWithinTransport()).
		WithField("dst", fmt.Sprintf("%s://%s", destRef.Transport().Name(), destRef.StringWithinTransport())).
		Info("pushing cache image to registry")
	return retry.RetryIfNecessary(ctx, func() error {
		_, err := copy.Image(ctx, policyCtx, destRef, srcRef, &copy.Options{
			SourceCtx:          &types.SystemContext{},
			DestinationCtx:     &s.systemContext,
			ImageListSelection: copy.CopySystemImage,
			ReportWriter:       os.Stdout,
		})
		return err
	}, &retry.RetryOptions{MaxRetry: 10, Delay: time.Second})
}
