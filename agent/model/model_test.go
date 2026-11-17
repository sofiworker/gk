package model

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
)

func TestParametersValidate(t *testing.T) {
	zero, negative, nan := 0.0, -1.0, math.NaN()
	count := 0
	empty := []string{}
	for _, tt := range []struct {
		name string
		p    Parameters
		bad  bool
	}{
		{"omitted", Parameters{}, false},
		{"explicit zero", Parameters{Temperature: &zero, TopP: &zero, Stop: &empty}, false},
		{"negative temperature", Parameters{Temperature: &negative}, true},
		{"nan", Parameters{TopP: &nan}, true},
		{"zero budget", Parameters{MaxOutputTokens: &count}, true},
		{"named missing name", Parameters{ToolChoice: &ToolChoice{Mode: ToolNamed}}, true},
		{"named", Parameters{ToolChoice: &ToolChoice{Mode: ToolNamed, Name: "read"}}, false},
		{"auto with name", Parameters{ToolChoice: &ToolChoice{Mode: ToolAuto, Name: "read"}}, true},
		{"schema", Parameters{OutputFormat: &OutputFormat{Kind: FormatSchema, Name: "answer", Schema: json.RawMessage(`{"type":"object"}`)}}, false},
		{"invalid schema", Parameters{OutputFormat: &OutputFormat{Kind: FormatSchema, Name: "answer", Schema: json.RawMessage(`{`)}}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.p.Validate()
			if (err != nil) != tt.bad || (err != nil && !errors.Is(err, ErrParameters)) {
				t.Fatalf("Validate() = %v", err)
			}
		})
	}
}
