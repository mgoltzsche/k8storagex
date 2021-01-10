package main

import (
	"os"

	"github.com/containers/buildah"
	"github.com/containers/storage/pkg/unshare"
	"github.com/sirupsen/logrus"
)

func main() {
	if buildah.InitReexec() {
		return
	}
	unshare.MaybeReexecUsingUserNamespace(false)
	if err := Execute(os.Stdout); err != nil {
		logrus.Fatal(err)
	}
}
