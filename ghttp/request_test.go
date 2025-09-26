package ghttp

import (
	"testing"
)

func TestNewRequest(t *testing.T) {
	request := NewRequest()
	request.SetUrl("https://www.google.com").JSON(map[string]interface{}{})
}
