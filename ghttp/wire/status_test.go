package wire

import (
	"errors"
	"fmt"
	"testing"
)

// statusErr 是实现 StatusCoder 的测试错误。
// statusErr is a test error implementing StatusCoder.
type statusErr struct{ code int }

func (e *statusErr) Error() string   { return fmt.Sprintf("status %d", e.code) }
func (e *statusErr) HTTPStatus() int { return e.code }

// TestStatusOf 覆盖 nil、普通错误、StatusCoder 及其包装。
// TestStatusOf covers nil, plain errors, StatusCoder and wrapped ones.
func TestStatusOf(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"plain", errors.New("boom"), 0},
		{"status coder", &statusErr{404}, 404},
		{"wrapped once", fmt.Errorf("ctx: %w", &statusErr{502}), 502},
		{"wrapped twice", fmt.Errorf("a: %w", fmt.Errorf("b: %w", &statusErr{429})), 429},
		{"joined", errors.Join(errors.New("x"), &statusErr{503}), 503},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StatusOf(tt.err); got != tt.want {
				t.Fatalf("StatusOf() = %d, want %d", got, tt.want)
			}
		})
	}
}
