package template

import (
	"fmt"

	"github.com/a8m/envsubst/parse"
)

type Substitution struct {
	parser *parse.Parser
}

func NewSubstitution(name string, values map[string]string) *Substitution {
	restrictions := &parse.Restrictions{NoUnset: true, NoEmpty: true}
	env := make([]string, 0, len(values))
	for k, v := range values {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	return &Substitution{parser: &parse.Parser{Name: name, Env: env, Restrict: restrictions, Mode: parse.AllErrors}}
}

func (s Substitution) SubstituteString(tpl string) (string, error) {
	return s.parser.Parse(tpl)
}

func (s Substitution) SubstituteMap(tpl map[string]interface{}) (map[string]interface{}, error) {
	return s.substituteMap(tpl, "")
}

func (s Substitution) substitute(tpl interface{}, path string) (interface{}, error) {
	switch c := tpl.(type) {
	case string:
		return s.SubstituteString(c)
	case map[string]interface{}:
		return s.substituteMap(c, path)
	case []interface{}:
		return s.substituteSlice(c, path)
	default:
		return tpl, nil
	}
}

func (s Substitution) substituteSlice(tpl []interface{}, parentPath string) (r []interface{}, err error) {
	r = make([]interface{}, len(tpl))
	for i, v := range tpl {
		path := fmt.Sprintf("%s[%d]", parentPath, i)
		r[i], err = s.substitute(v, path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	return r, nil
}

func (s Substitution) substituteMap(tpl map[string]interface{}, parentPath string) (r map[string]interface{}, err error) {
	r = map[string]interface{}{}
	for k, v := range tpl {
		path := fmt.Sprintf("%s.%s", parentPath, k)
		r[k], err = s.substitute(v, path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	return r, nil
}
