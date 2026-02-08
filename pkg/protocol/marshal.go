package protocol

import (
	"bytes"
	"fmt"
	"reflect"
)

const tagName = "mc"

// Marshal encodes a Packet struct into bytes using mc struct tags.
func Marshal(p Packet) ([]byte, error) {
	v := reflect.ValueOf(p)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("marshal: expected struct, got %s", v.Kind())
	}

	var buf bytes.Buffer
	t := v.Type()

	for i := range t.NumField() {
		field := t.Field(i)
		tag := field.Tag.Get(tagName)
		if tag == "" || tag == "-" {
			continue
		}

		if err := WriteField(&buf, tag, v.Field(i).Interface()); err != nil {
			return nil, fmt.Errorf("marshal field %s: %w", field.Name, err)
		}
	}

	return buf.Bytes(), nil
}

// Unmarshal decodes bytes into a Packet struct using mc struct tags.
func Unmarshal(data []byte, p Packet) error {
	v := reflect.ValueOf(p)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return fmt.Errorf("unmarshal: expected non-nil pointer, got %T", p)
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("unmarshal: expected pointer to struct, got pointer to %s", v.Kind())
	}

	r := bytes.NewReader(data)
	t := v.Type()

	for i := range t.NumField() {
		field := t.Field(i)
		tag := field.Tag.Get(tagName)
		if tag == "" || tag == "-" {
			continue
		}

		val, err := ReadField(r, tag)
		if err != nil {
			return fmt.Errorf("unmarshal field %s: %w", field.Name, err)
		}

		fv := v.Field(i)
		rv := reflect.ValueOf(val)
		if !rv.Type().AssignableTo(fv.Type()) {
			return fmt.Errorf("unmarshal field %s: cannot assign %s to %s", field.Name, rv.Type(), fv.Type())
		}
		fv.Set(rv)
	}

	return nil
}
