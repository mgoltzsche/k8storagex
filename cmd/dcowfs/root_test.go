package main

/*
import (
	"bytes"
	"io/ioutil"
	"os"
	"testing"

	"github.com/containers/storage/pkg/reexec"
	"github.com/stretchr/testify/require"
)

func init() {
	if reexec.Init() {
		os.Exit(0)
	}
}

func TestTemplateCommand(t *testing.T) {
	dir, err := ioutil.TempDir("", "dlcachefs-test-")
	require.NoError(t, err)
	defer os.RemoveAll(dir)
	out := runCommand(t, "dlcachefs", "--debug", "mount", "myvol", "mycache", "")
	require.NotEmpty(t, out, "mount output")
}

func runCommand(t *testing.T, args ...string) string {
	var out bytes.Buffer
	os.Args = args
	err := Execute(&out)
	require.NoError(t, err, "%+v", args)
	return out.String()
}
*/
