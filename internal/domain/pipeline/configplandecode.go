// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package pipeline

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// DecodeConfigPlan rejects duplicate consumed members before decoding can merge
// or overwrite them. Callers must still ValidateConfigPlan (directly or through
// a plan constructor) before use. Unknown extensions retain encoding/json behavior.
func DecodeConfigPlan(raw string) (ConfigPlan, error) {
	var body json.RawMessage
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		return ConfigPlan{}, fmt.Errorf("parse config-plan-json: %w: %w", err, errs.ErrInvalidConfig)
	}

	if err := checkConfigPlanMembers(body, reflect.TypeFor[ConfigPlan](), "config-plan"); err != nil {
		return ConfigPlan{}, fmt.Errorf("parse config-plan-json: %w", err)
	}

	var plan ConfigPlan
	if err := json.Unmarshal(body, &plan); err != nil {
		return ConfigPlan{}, fmt.Errorf("parse config-plan-json: %w: %w", err, errs.ErrInvalidConfig)
	}

	return plan, nil
}

// DecodeStagePlan decodes one stage plan into out, refusing unknown members and
// trailing values. A stage plan is produced and consumed by the same release,
// so an unknown member is a typo or a stale shape, and decoding past it would
// read a target as not running.
func DecodeStagePlan(raw, label string, out any) error {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("parse %s: not valid contract JSON: %w: %w", label, err, errs.ErrInvalidConfig)
	}

	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("parse %s: trailing JSON value: %w", label, errs.ErrInvalidConfig)
	}

	return nil
}

// This walk is limited to ConfigPlan's tagged structs, pointers, slices and
// string maps. It is not JSON schema validation or a general struct-field resolver:
// these wire types have no embedded fields, ambiguous tags or custom unmarshallers.
// RawMessage keeps unknown subtrees opaque; ordinary decoding owns shape errors.
func checkConfigPlanMembers(body json.RawMessage, shape reflect.Type, path string) error {
	for shape.Kind() == reflect.Pointer {
		shape = shape.Elem()
	}

	body = bytes.TrimSpace(body)
	if shape.Kind() == reflect.Slice && body[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(body, &items); err != nil {
			return err
		}

		for index, item := range items {
			if err := checkConfigPlanMembers(item, shape.Elem(), fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	}

	if (shape.Kind() != reflect.Struct && shape.Kind() != reflect.Map) || body[0] != '{' {
		return nil
	}

	return checkConfigPlanObject(body, shape, path)
}

func checkConfigPlanObject(body json.RawMessage, shape reflect.Type, path string) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	if _, err := decoder.Token(); err != nil {
		return err
	}

	seen := make(map[string]bool)

	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}

		name, _ := token.(string) // Syntax was checked before walking the object.

		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}

		member, child := configPlanMember(shape, name)
		if child == nil {
			continue
		}

		memberPath := path + "." + member
		if shape.Kind() == reflect.Map {
			memberPath = fmt.Sprintf("%s[%q]", path, member)
		}

		if seen[member] {
			return fmt.Errorf("%s has duplicate consumed member: %w", memberPath, errs.ErrInvalidConfig)
		}

		seen[member] = true

		if err := checkConfigPlanMembers(value, child, memberPath); err != nil {
			return err
		}
	}

	return nil
}

func configPlanMember(shape reflect.Type, name string) (string, reflect.Type) {
	if shape.Kind() == reflect.Map {
		// Map keys are consumed literally, not case-folded like struct fields.
		return name, shape.Elem()
	}

	for index := range shape.NumField() {
		field := shape.Field(index)
		tag, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		// ConfigPlan's tags are unique even under encoding/json's Unicode fold.
		if strings.EqualFold(name, tag) {
			return tag, field.Type
		}
	}

	return "", nil
}
