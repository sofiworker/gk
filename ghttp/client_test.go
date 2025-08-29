package ghttp

import (
	"fmt"
	"net/http"
	"testing"
)

func TestClient(t *testing.T) {
	client := NewClient()
	r, err := client.R().SetUrl("https://www.baidu.com").SetMethod(http.MethodGet).SetEnableDumpBody(true).Done()
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(string(r.BodyRaw))
}

func TestUrl(t *testing.T) {
	baseURLs := []string{
		"https://www.example.com",
		"",
	}
	paths := []string{
		"/path/to/resource",
		"https://another-example.com/path",
	}

	for _, baseURL := range baseURLs {
		for _, path := range paths {
			fullURL, err := ConstructURL(baseURL, path)
			if err != nil {
				fmt.Printf("构建完整URL失败: %v\n", err)
				continue
			}
			fmt.Printf("BaseURL: '%s', Path: '%s' => 完整URL: '%s'\n", baseURL, path, fullURL)
		}
	}
}
