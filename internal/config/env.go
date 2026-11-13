package config

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// EnvPrefix starts the name of every environment variable the proxy reads.
// LegacyEnvPrefix is the prefix of release 0.1.0. A LLM_PROXY_ name is still
// read when its SPYGLASS_ name is unset, and Options.Deprecated reports it
// (ADR 0054).
const (
	EnvPrefix       = "SPYGLASS"
	LegacyEnvPrefix = "LLM_PROXY"
)

// LegacyEnvName returns the deprecated LLM_PROXY_ name of a SPYGLASS_ variable.
// It returns false for any other name.
func LegacyEnvName(name string) (string, bool) {
	rest, ok := strings.CutPrefix(name, EnvPrefix+"_")
	if !ok || rest == "" {
		return "", false
	}
	return LegacyEnvPrefix + "_" + rest, true
}

// lookupEnv reads the variable name. When name is a SPYGLASS_ variable and is
// unset, it reads the LLM_PROXY_ name in its place and reports that name to
// o.Deprecated. The set SPYGLASS_ name always wins.
func (o Options) lookupEnv(name string) (string, bool) {
	if v, ok := o.LookupEnv(name); ok {
		return v, true
	}
	legacy, ok := LegacyEnvName(name)
	if !ok {
		return "", false
	}
	v, ok := o.LookupEnv(legacy)
	if ok {
		o.deprecated(legacy, name)
	}
	return v, ok
}

func (o Options) deprecated(legacy, name string) {
	if o.Deprecated != nil {
		o.Deprecated(legacy, name)
	}
}

// applyEnv overrides scalar keys from SPYGLASS_<SECTION>_<KEY> variables, or
// from the deprecated LLM_PROXY_<SECTION>_<KEY> name when the SPYGLASS_ one is
// unset. The names come from the yaml tags of Config, so every key has one.
// Lists and maps are skipped. A secret reference is replaced whole by its _ENV
// and _FILE parts, so overriding one part drops the other part set in the file.
// The parts are read under one prefix: when either SPYGLASS_ part is set, the
// LLM_PROXY_ parts are ignored.
func applyEnv(cfg *Config, o Options) []error {
	var errs []error
	walkLeaves(reflect.ValueOf(cfg).Elem(), []string{EnvPrefix}, func(name string, v reflect.Value) {
		if v.Type() == refType {
			if ref, ok := refFromEnv(name, o); ok {
				v.Set(reflect.ValueOf(ref))
			}
			return
		}
		used := name
		raw, ok := o.LookupEnv(name)
		if !ok {
			legacy, _ := LegacyEnvName(name)
			if raw, ok = o.LookupEnv(legacy); !ok {
				return
			}
			used = legacy
			o.deprecated(legacy, name)
		}
		if err := setLeaf(v, raw); err != nil {
			errs = append(errs, fmt.Errorf("environment variable %s: %w", used, err))
		}
	})
	return errs
}

// refFromEnv reads the _ENV and _FILE parts of the secret reference name. It
// reads the SPYGLASS_ parts, or the LLM_PROXY_ parts when neither SPYGLASS_
// part is set. It returns false when no part is set.
func refFromEnv(name string, o Options) (Ref, bool) {
	env, envOK := o.LookupEnv(name + "_ENV")
	file, fileOK := o.LookupEnv(name + "_FILE")
	if envOK || fileOK {
		return Ref{Env: env, File: file}, true
	}
	legacy, _ := LegacyEnvName(name)
	env, envOK = o.LookupEnv(legacy + "_ENV")
	file, fileOK = o.LookupEnv(legacy + "_FILE")
	if envOK {
		o.deprecated(legacy+"_ENV", name+"_ENV")
	}
	if fileOK {
		o.deprecated(legacy+"_FILE", name+"_FILE")
	}
	return Ref{Env: env, File: file}, envOK || fileOK
}

var (
	durationType = reflect.TypeFor[Duration]()
	sizeType     = reflect.TypeFor[Size]()
	refType      = reflect.TypeFor[Ref]()
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
		case f.Type == durationType, f.Type == sizeType, f.Type == refType:
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
