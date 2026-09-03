package client

import (
	"reflect"
	"strings"
	"testing"
)

// jsonKeys indexes a struct's json tag names, following the anonymous embeds
// whose fields marshal flat.
func jsonKeys(tp reflect.Type) map[string]bool {
	keys := map[string]bool{}
	for i := 0; i < tp.NumField(); i++ {
		field := tp.Field(i)
		if field.Anonymous && field.Tag.Get("json") == "" {
			for k := range jsonKeys(field.Type) {
				keys[k] = true
			}
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name != "" && name != "-" {
			keys[name] = true
		}
	}
	return keys
}

// The serializer's contract, mirrored: everything writable is readable back,
// EXCEPT the ten secrets, which surface only as a `<key>_set` presence
// boolean. The per-field reflective suites guard input→wire and
// response→state, but not this partition — a field added to the write side
// without its read side would pass all of them while shipping with import and
// drift detection silently broken for that attribute.
func TestInstanceWritableFieldsAreReadable(t *testing.T) {
	secrets := make(map[string]bool, len(InstanceSecretKeys))
	for _, key := range InstanceSecretKeys {
		secrets[key] = true
	}

	writable := jsonKeys(reflect.TypeOf(UpdateInstanceInput{}))
	readable := jsonKeys(reflect.TypeOf(Instance{}))

	for key := range writable {
		switch {
		case secrets[key]:
			if readable[key] {
				t.Errorf("%s is write-only but Instance declares it readable", key)
			}
			if !readable[key+"_set"] {
				t.Errorf("%s has no %s_set presence field on Instance", key, key)
			}
		case !readable[key]:
			t.Errorf("%s is writable but not readable from Instance — import and refresh cannot see it", key)
		}
	}

	// The list itself has to name real fields, or a typo in it would silently
	// exempt a credential from every guard that derives from it.
	for _, key := range InstanceSecretKeys {
		if !writable[key] {
			t.Errorf("InstanceSecretKeys names %q, which is not a writable field", key)
		}
	}
}
