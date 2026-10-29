package pdfops

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// formFieldKinds are the JSON arrays of fields inside one exported form,
// in the shape the API's fill form machinery accepts.
var formFieldKinds = []string{"textfield", "datefield", "checkbox", "combobox", "listbox", "radiobuttongroup"}

// overlayFormFields sets each requested field's value in the form JSON the
// API exports, matching by field name or numeric field id. It returns the
// updated JSON and how many field instances were set; a requested field
// that matches nothing is a rejection naming the available fields.
func overlayFormFields(exported []byte, fields map[string]any) ([]byte, int, error) {
	var doc map[string]any
	if err := json.Unmarshal(exported, &doc); err != nil {
		return nil, 0, fmt.Errorf("decode exported form: %w", err)
	}

	filled := 0
	matched := map[string]bool{}
	var known []string
	for _, f := range formArray(doc) {
		for _, kind := range formFieldKinds {
			for _, field := range fieldArray(f, kind) {
				if name, _ := field["name"].(string); name != "" {
					known = append(known, name)
				}
				value, ok := requestedValue(fields, field, matched)
				if !ok {
					continue
				}
				set, err := fieldValue(kind, field, value)
				if err != nil {
					return nil, 0, err
				}
				field["value"] = set
				filled++
			}
		}
	}

	if unknown := unmatched(fields, matched); len(unknown) > 0 {
		sort.Strings(known)
		sort.Strings(unknown)
		return nil, 0, rejectf("unknown form fields: %s — this form offers: %s",
			strings.Join(unknown, ", "), strings.Join(known, ", "))
	}

	out, err := json.Marshal(doc)
	if err != nil {
		return nil, 0, fmt.Errorf("encode form data: %w", err)
	}
	return out, filled, nil
}

// formArray returns the exported document's forms array.
func formArray(doc map[string]any) []map[string]any {
	forms, _ := doc["forms"].([]any)
	out := make([]map[string]any, 0, len(forms))
	for _, f := range forms {
		if form, ok := f.(map[string]any); ok {
			out = append(out, form)
		}
	}
	return out
}

// fieldArray returns one form's field objects of the given kind.
func fieldArray(form map[string]any, kind string) []map[string]any {
	entries, _ := form[kind].([]any)
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		if field, ok := e.(map[string]any); ok {
			out = append(out, field)
		}
	}
	return out
}

// requestedValue looks up the requested value for one exported field,
// matching by field name first and by numeric field id second; a match is
// recorded so unmatched requests can be reported.
func requestedValue(fields map[string]any, field map[string]any, matched map[string]bool) (any, bool) {
	name, _ := field["name"].(string)
	id, _ := field["id"].(string)
	for _, key := range []string{name, id} {
		if key == "" {
			continue
		}
		if v, ok := fields[key]; ok {
			matched[key] = true
			return v, true
		}
	}
	return nil, false
}

// unmatched returns the requested field names that matched no exported
// field.
func unmatched(fields map[string]any, matched map[string]bool) []string {
	var unknown []string
	for name := range fields {
		if !matched[name] {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	return unknown
}

// fieldValue coerces a requested value into the JSON shape pdfcpu expects
// for the field kind; an impossible shape is a rejection.
func fieldValue(kind string, field map[string]any, value any) (any, error) {
	name, _ := field["name"].(string)
	switch kind {
	case "checkbox":
		return checkboxValue(name, value)
	case "listbox":
		return listValues(name, value)
	default:
		// Text, date, combo box, and radio button groups take strings;
		// scalar numbers and booleans are stringified for convenience.
		return fmt.Sprintf("%v", value), nil
	}
}

// checkboxValue coerces a requested checkbox value: booleans pass, the
// strings "true"/"false" (case-insensitive) convert, anything else is a
// rejection.
func checkboxValue(name string, value any) (bool, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "yes", "on":
			return true, nil
		case "false", "no", "off", "":
			return false, nil
		}
	}
	return false, rejectf("form field %q is a checkbox — set it to true or false", name)
}

// listValues coerces a requested list-box value: an array passes through, a
// single value becomes a one-element selection.
func listValues(name string, value any) ([]any, error) {
	switch v := value.(type) {
	case []any:
		if len(v) == 0 {
			return nil, rejectf("form field %q needs at least one selected value", name)
		}
		return v, nil
	case string:
		if v == "" {
			return nil, rejectf("form field %q needs at least one selected value", name)
		}
		return []any{v}, nil
	}
	return nil, rejectf("form field %q is a list box — set it to a value or an array of values", name)
}
