package main

import "github.com/sofiworker/gk/ghttp/gserver"

func main() {

	server := gserver.NewServer()
	server.ANY("/", func(ctx *gserver.Context) {
		ctx.RespAuto("hello world")
	})

	group1 := server.Group("/group1")
	group1.GET("/info", func(ctx *gserver.Context) {

	})
	group1.POST("/create", func(ctx *gserver.Context) {

	})
	group1.HEAD("/head", func(ctx *gserver.Context) {

	})

	err := server.Run(":8080")
	if err != nil {
		panic(err)
	}
}
