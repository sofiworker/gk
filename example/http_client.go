//go:build ignore
// +build ignore

package main

import (
	"fmt"

	"github.com/sofiworker/gk/ghttp/gclient"
)

func main() {
	client := gclient.NewClient()
	client.SetBaseUrl("http://127.0.0.1:8080")

	response, err := client.R().
		SetBody(map[string]interface{}{
			"name": "gk",
			"age":  18,
		}).
		Post("/api/user")
	if err != nil {
		panic(err)
	}
	fmt.Println(response.String())
}
