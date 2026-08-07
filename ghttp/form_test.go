package ghttp

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMultipartFormWithFile(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	_ = writer.WriteField("name", "Alice")
	part, _ := writer.CreateFormFile("avatar", "avatar.png")
	part.Write([]byte("fake-image-data"))
	writer.Close()

	req := httptest.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	type uploadInput struct {
		Body struct {
			Name   string      `form:"name"`
			Avatar *FileHeader `form:"avatar"`
		}
	}

	var input uploadInput
	err := parseInput(req, &input)
	require.NoError(t, err)

	assert.Equal(t, "Alice", input.Body.Name)
	require.NotNil(t, input.Body.Avatar)
	assert.Equal(t, "avatar.png", input.Body.Avatar.Filename)
}

func TestParseMultipartFormMultipleFiles(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part1, _ := writer.CreateFormFile("files", "a.txt")
	part1.Write([]byte("aaa"))
	part2, _ := writer.CreateFormFile("files", "b.txt")
	part2.Write([]byte("bbb"))
	writer.Close()

	req := httptest.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	type uploadInput struct {
		Body struct {
			Files []*FileHeader `form:"files"`
		}
	}

	var input uploadInput
	err := parseInput(req, &input)
	require.NoError(t, err)
	assert.Len(t, input.Body.Files, 2)
	assert.Equal(t, "a.txt", input.Body.Files[0].Filename)
	assert.Equal(t, "b.txt", input.Body.Files[1].Filename)
}

func TestParseMultipartFormValues(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	writer.WriteField("name", "Bob")
	writer.WriteField("age", "30")
	writer.Close()

	req := httptest.NewRequest("POST", "/submit", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	type formInput struct {
		Body struct {
			Name string `form:"name"`
			Age  int    `form:"age"`
		}
	}

	var input formInput
	err := parseInput(req, &input)
	require.NoError(t, err)
	assert.Equal(t, "Bob", input.Body.Name)
	assert.Equal(t, 30, input.Body.Age)
}

func TestParseURLEncodedFormValues(t *testing.T) {
	req := httptest.NewRequest("POST", "/submit", bytes.NewBufferString("name=Carol&age=28"))
	req.Header.Set("Content-Type", MIMEPOSTForm)

	type formInput struct {
		Body struct {
			Name string `form:"name"`
			Age  int    `form:"age"`
		}
	}

	var input formInput
	err := parseInput(req, &input)
	require.NoError(t, err)
	assert.Equal(t, "Carol", input.Body.Name)
	assert.Equal(t, 28, input.Body.Age)
}

func TestURLEncodedFormWithoutTagsReturns400(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	type input struct {
		Params
		Body struct {
			Name string `json:"name"`
		}
	}
	Route[input, struct{}](app).POST("/users").To(func(context.Context, input) (struct{}, error) {
		return struct{}{}, nil
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/users", bytes.NewBufferString("name=alice"))
	req.Header.Set("Content-Type", MIMEPOSTForm)
	app.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
}

func TestParseMultipartFormEmptyBody(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.Close()

	req := httptest.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	type input struct {
		Body struct {
			Name string `form:"name"`
		}
	}

	var inputData input
	err := parseInput(req, &inputData)
	require.NoError(t, err)
	assert.Empty(t, inputData.Body.Name)
}
