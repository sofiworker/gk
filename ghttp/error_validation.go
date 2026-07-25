package ghttp

import (
	"context"
	"errors"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"unicode"

	playgroundValidator "github.com/go-playground/validator/v10"
)

type validationStageError struct {
	err   error
	input any
}

func (e validationStageError) Error() string { return e.err.Error() }
func (e validationStageError) Unwrap() error { return e.err }

type defaultValidationErrorAdapter struct{}

func (defaultValidationErrorAdapter) AdaptValidationError(_ context.Context, err error) (ValidationResult, bool) {
	var staged validationStageError
	if !errors.As(err, &staged) {
		return ValidationResult{}, false
	}
	var validationErrors playgroundValidator.ValidationErrors
	if !errors.As(staged.err, &validationErrors) {
		return ValidationResult{}, false
	}
	details := make([]ErrorDetail, 0, len(validationErrors))
	for _, fieldError := range validationErrors {
		args := map[string]any{}
		if fieldError.Param() != "" {
			args["param"] = fieldError.Param()
		}
		details = append(details, ErrorDetail{
			Location:  validationLocation(reflect.TypeOf(staged.input), fieldError.StructNamespace()),
			MessageID: validationMessageID(fieldError.Tag()),
			Args:      args,
		})
	}
	sort.SliceStable(details, func(i, j int) bool {
		if details[i].Location == details[j].Location {
			return details[i].MessageID < details[j].MessageID
		}
		return details[i].Location < details[j].Location
	})
	return ValidationResult{Details: details}, true
}

func validationMessageID(tag string) string {
	known := map[string]string{
		"required": "required", "email": "email", "min": "min", "max": "max",
		"oneof": "one_of", "len": "length",
	}
	if normalized, ok := known[tag]; ok {
		return "validation." + normalized
	}
	var builder strings.Builder
	previousUnderscore := false
	for _, r := range tag {
		switch {
		case unicode.IsUpper(r):
			if builder.Len() > 0 && !previousUnderscore {
				builder.WriteByte('_')
			}
			builder.WriteRune(unicode.ToLower(r))
			previousUnderscore = false
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			builder.WriteRune(r)
			previousUnderscore = false
		default:
			if builder.Len() > 0 && !previousUnderscore {
				builder.WriteByte('_')
				previousUnderscore = true
			}
		}
	}
	normalized := strings.Trim(builder.String(), "_")
	if normalized == "" || !regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`).MatchString(normalized) {
		normalized = "unknown"
	}
	return "validation." + normalized
}

func validationLocation(root reflect.Type, namespace string) string {
	for root != nil && root.Kind() == reflect.Pointer {
		root = root.Elem()
	}
	parts := strings.Split(namespace, ".")
	if len(parts) > 0 && root != nil && parts[0] == root.Name() {
		parts = parts[1:]
	}
	source := "body"
	path := make([]string, 0, len(parts))
	current := root
	for _, part := range parts {
		fieldName, suffix := splitFieldIndex(part)
		for current != nil && current.Kind() == reflect.Pointer {
			current = current.Elem()
		}
		if current != nil && current.Kind() == reflect.Slice {
			current = current.Elem()
		}
		protocolName := lowerFirst(fieldName)
		if current != nil && current.Kind() == reflect.Struct {
			if field, ok := current.FieldByName(fieldName); ok {
				if len(path) == 0 {
					source, protocolName = protocolBinding(field)
				} else if name := tagName(field.Tag.Get("json")); name != "" {
					protocolName = name
				}
				current = field.Type
			}
		}
		path = append(path, protocolName+suffix)
	}
	return source + "." + strings.Join(path, ".")
}

func protocolBinding(field reflect.StructField) (string, string) {
	for _, source := range []string{"path", "query", "header", "cookie", "json"} {
		if name := tagName(field.Tag.Get(source)); name != "" {
			if source == "json" {
				source = "body"
			}
			return source, name
		}
	}
	return "body", lowerFirst(field.Name)
}

func tagName(tag string) string {
	name, _, _ := strings.Cut(tag, ",")
	if name == "-" {
		return ""
	}
	return name
}

func splitFieldIndex(part string) (string, string) {
	if index := strings.IndexByte(part, '['); index >= 0 {
		return part[:index], part[index:]
	}
	return part, ""
}

func lowerFirst(value string) string {
	if value == "" {
		return ""
	}
	runes := []rune(value)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}
