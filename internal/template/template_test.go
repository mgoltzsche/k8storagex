package template

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSubstitution(t *testing.T) {
	now := time.Now()
	values := map[string]string{"var_a": "val_a", "var_b": "val_b"}
	tpl := map[string]interface{}{"a": "substitute ${var_a}", "b": map[string]interface{}{"ba": "val ${var_b}"}, "x": now, "list": []interface{}{"entry ${var_a}", "last entry"}}
	expected := map[string]interface{}{"a": "substitute val_a", "b": map[string]interface{}{"ba": "val val_b"}, "x": now, "list": []interface{}{"entry val_a", "last entry"}}
	actual, err := NewSubstitution("testsubst", values).SubstituteMap(tpl)
	require.NoError(t, err)
	require.Equal(t, expected, actual)
}

func TestSubstitutionFailOnUndefinedVariable(t *testing.T) {
	values := map[string]string{"var_a": "val_a"}
	tpl := map[string]interface{}{"a": "${var_non_existing}"}
	actual, err := NewSubstitution("testsubst", values).SubstituteMap(tpl)
	require.Error(t, err)
	require.Nil(t, actual)
}
