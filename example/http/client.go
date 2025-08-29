package main

import (
	"gk/ghttp"
	"log"
	"time"
)

func main() {
	client := ghttp.NewClient()
	client.SetBaseUrl("http://127.0.0.1:8080")
	r := client.R()
	r.SetHeader("Content-Type", "application/json").SetHeader("Accept", "application/json").
		SetHeader("User-Agent", "Go Client").
		SetHeader("Accept-Encoding", "gzip").SetMaxRedirects(1).SetTimeout(3 * time.Second)
	response, err := r.Get()
	if err != nil {
		log.Fatal(err)
	}
	defer response.Close()
	var data map[string]interface{}
	err = response.SetDecoder(ghttp.NewJsonDecoder()).Unmarshal(&data)
	if err != nil {
		log.Fatal(err)
	}
}
