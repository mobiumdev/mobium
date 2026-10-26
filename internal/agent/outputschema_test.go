package agent

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"
)

// Every tool's outputSchema holds the values its type takes, as
// encoding/json writes them: empty, with every slice and pointer nil, and
// filled, with every field set and every slice and map holding one element.
// A client may refuse a result that does not match its tool's schema, so a
// schema that is wrong is worse than none.
func TestEveryOutputSchemaHoldsItsTypesValues(t *testing.T) {
	if len(outputZeros) != len(outputSchemas) {
		t.Fatalf("%d schemas and %d types: regenerate with make api", len(outputSchemas), len(outputZeros))
	}
	names := make([]string, 0, len(outputZeros))
	for n := range outputZeros {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		schema := OutputSchema(name)
		if schema == nil || schema["type"] != "object" {
			t.Errorf("%s: schema %v is no object", name, schema)
			continue
		}
		zero := outputZeros[name]()
		for which, v := range map[string]interface{}{"empty": zero, "filled": filled(reflect.TypeOf(zero), 0).Interface()} {
			raw, err := json.Marshal(v)
			if err != nil {
				t.Errorf("%s %s: %v", name, which, err)
				continue
			}
			var doc interface{}
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}
			if err := validate(schema, doc, "$"); err != nil {
				t.Errorf("%s, %s: %v\n%s", name, which, err, raw)
			}
		}
	}
}

// The validator is held to a value it must refuse, so its passes mean
// something: a required field missing, and a string where a number goes.
func TestTheValidatorRefuses(t *testing.T) {
	s := map[string]interface{}{"type": "object", "required": []interface{}{"n"},
		"properties": map[string]interface{}{"n": map[string]interface{}{"type": "integer"}}}
	if validate(s, map[string]interface{}{}, "$") == nil {
		t.Error("a missing required field passed")
	}
	if validate(s, map[string]interface{}{"n": "1"}, "$") == nil {
		t.Error("a string passed as an integer")
	}
	if validate(s, map[string]interface{}{"n": 1.0}, "$") != nil {
		t.Error("an integer was refused")
	}
}

func TestTheToolsListCarriesTheSchemas(t *testing.T) {
	with := 0
	for _, tl := range GetToolSchemas() {
		if tl.OutputSchema != nil {
			with++
		}
	}
	if with != len(outputSchemas) || with < 60 {
		t.Errorf("%d tools list an outputSchema, %d generated", with, len(outputSchemas))
	}
}

// filled is a value of t with every field set.
func filled(t reflect.Type, depth int) reflect.Value {
	v := reflect.New(t).Elem()
	if depth > 6 {
		return v
	}
	if t == reflect.TypeOf(time.Time{}) {
		return reflect.ValueOf(time.Unix(1, 0).UTC())
	}
	switch t.Kind() {
	case reflect.String:
		v.SetString("x")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(1)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1.5)
	case reflect.Pointer:
		v.Set(filled(t.Elem(), depth+1).Addr())
	case reflect.Slice:
		s := reflect.MakeSlice(t, 1, 1)
		s.Index(0).Set(filled(t.Elem(), depth+1))
		v.Set(s)
	case reflect.Map:
		m := reflect.MakeMap(t)
		if t.Key().Kind() == reflect.String {
			m.SetMapIndex(reflect.ValueOf("k").Convert(t.Key()), filled(t.Elem(), depth+1))
		}
		v.Set(m)
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if f := t.Field(i); f.IsExported() {
				v.Field(i).Set(filled(f.Type, depth+1))
			}
		}
	case reflect.Interface:
		if t.NumMethod() == 0 {
			v.Set(reflect.ValueOf("x"))
		}
	}
	return v
}

// validate holds doc to the part of JSON Schema the generator writes.
func validate(schema map[string]interface{}, doc interface{}, at string) error {
	if len(schema) == 0 {
		return nil
	}
	types := []string{}
	switch t := schema["type"].(type) {
	case string:
		types = append(types, t)
	case []interface{}:
		for _, x := range t {
			types = append(types, x.(string))
		}
	}
	ok := len(types) == 0
	for _, ty := range types {
		if fits(ty, doc) {
			ok = true
		}
	}
	if !ok {
		return fmt.Errorf("%s: %T %v is not %v", at, doc, doc, types)
	}
	switch d := doc.(type) {
	case map[string]interface{}:
		props, _ := schema["properties"].(map[string]interface{})
		if req, ok := schema["required"].([]interface{}); ok {
			for _, r := range req {
				if _, has := d[r.(string)]; !has {
					return fmt.Errorf("%s: required %q missing", at, r)
				}
			}
		}
		for k, v := range d {
			if ps, ok := props[k].(map[string]interface{}); ok {
				if err := validate(ps, v, at+"."+k); err != nil {
					return err
				}
			} else if ap, ok := schema["additionalProperties"].(map[string]interface{}); ok {
				if err := validate(ap, v, at+"."+k); err != nil {
					return err
				}
			}
		}
	case []interface{}:
		if items, ok := schema["items"].(map[string]interface{}); ok {
			for i, v := range d {
				if err := validate(items, v, fmt.Sprintf("%s[%d]", at, i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func fits(ty string, doc interface{}) bool {
	switch ty {
	case "null":
		return doc == nil
	case "object":
		_, ok := doc.(map[string]interface{})
		return ok
	case "array":
		_, ok := doc.([]interface{})
		return ok
	case "string":
		_, ok := doc.(string)
		return ok
	case "boolean":
		_, ok := doc.(bool)
		return ok
	case "number":
		_, ok := doc.(float64)
		return ok
	case "integer":
		f, ok := doc.(float64)
		return ok && f == float64(int64(f))
	}
	return false
}
