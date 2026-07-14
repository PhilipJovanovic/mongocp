package main

import (
	"encoding/json"
	"errors"
)

// GPT Actions frequently send JSON values in slightly wrong shapes: objects
// as JSON-encoded strings, or a single object where an array is expected.
// These types accept all of those instead of failing the request.

// flexObject unmarshals a JSON object or a string containing one.
type flexObject map[string]any

func (f *flexObject) UnmarshalJSON(b []byte) error {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err == nil {
		*f = m
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		var m2 map[string]any
		if err := json.Unmarshal([]byte(s), &m2); err == nil {
			*f = m2
			return nil
		}
		return errors.New("string value is not a JSON object; send a real JSON object like {\"field\": \"value\"}")
	}
	return errors.New("expected a JSON object like {\"field\": \"value\"}")
}

// flexObjectArray unmarshals an array of objects, a single object, or a
// string containing either.
type flexObjectArray []map[string]any

func (f *flexObjectArray) UnmarshalJSON(b []byte) error {
	var arr []map[string]any
	if err := json.Unmarshal(b, &arr); err == nil {
		*f = arr
		return nil
	}
	var one map[string]any
	if err := json.Unmarshal(b, &one); err == nil {
		*f = []map[string]any{one}
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		var arr2 []map[string]any
		if err := json.Unmarshal([]byte(s), &arr2); err == nil {
			*f = arr2
			return nil
		}
		var one2 map[string]any
		if err := json.Unmarshal([]byte(s), &one2); err == nil {
			*f = []map[string]any{one2}
			return nil
		}
		return errors.New("string value is not valid JSON; send a real JSON array of objects like [{\"field\": \"value\"}]")
	}
	return errors.New("expected a JSON array of objects like [{\"field\": \"value\"}]")
}
