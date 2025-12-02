//go:build ignore
// +build ignore

package main

import "github.com/sofiworker/gk/gconfig"

func main() {
	config, err := gconfig.New()
	if err != nil {
		panic(err)
	}
	err = config.Load()
	if err != nil {
		panic(err)
	}

	type App struct {
		Name string `json:"name"`
		Env  string `json:"env"`
		Port int    `json:"port"`
	}

	err = config.Unmarshal(&App{})
	if err != nil {
		panic(err)
	}
}
