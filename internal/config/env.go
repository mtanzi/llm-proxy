package config

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

const envPrefix = "LLM_PROXY"

// applyEnv overrides scalar keys from LLM_PROXY_<SECTION>_<KEY> variables. The
// names come from the yaml tags of Config, so every key has one. Lists and maps
// are skipped.
func applyEnv(cfg *Config, o Options) []error {
	var errs []error
	walkLeaves(reflect.ValueOf(cfg).Elem(), []string{envPrefix}, func(name string, v reflect.Value) {
		raw, ok := o.LookupEnv(name)
		if !ok {
			return
		}
		if err := setLeaf(v, raw); err != nil {
			errs = append(errs, fmt.Errorf("environment variable %s: %w", name, err))
		}
	})
	return errs
}

var (
	durationType = reflect.TypeFor[Duration]()
	sizeType     = reflect.TypeFor[Size]()
)

// walkLeaves calls fn with the variable name and field of each overridable key.
func walkLeaves(v reflect.Value, path []string, fn func(name string, v reflect.Value)) {
	t := v.Type()
	for i := range t.NumField() {
		f := t.Field(i)
		tag, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if !f.IsExported() || tag == "" || tag == "-" {
			continue
		}
		p := append(path[:len(path):len(path)], strings.ToUpper(tag))
		fv := v.Field(i)
		switch {
		case f.Type == durationType, f.Type == sizeType:
			fn(strings.Join(p, "_"), fv)
		case f.Type.Kind() == reflect.Struct:
			walkLeaves(fv, p, fn)
		case f.Type.Kind() == reflect.String, f.Type.Kind() == reflect.Int, f.Type.Kind() == reflect.Bool:
			fn(strings.Join(p, "_"), fv)
		}
	}
}

func setLeaf(v reflect.Value, raw string) error {
	switch v.Type() {
	case durationType:
		d, err := ParseDuration(raw)
		if err != nil {
			return err
		}
		v.Set(reflect.ValueOf(d))
		return nil
	case sizeType:
		s, err := ParseSize(raw)
		if err != nil {
			return err
		}
		v.Set(reflect.ValueOf(s))
		return nil
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString(raw)
	case reflect.Int:
		n, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("invalid number %q", raw)
		}
		v.SetInt(int64(n))
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return fmt.Errorf("invalid boolean %q", raw)
		}
		v.SetBool(b)
	}
	return nil
}
