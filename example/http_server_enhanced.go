package main

import (
	"github.com/sofiworker/gk/ghttp/gserver"
)

type User struct {
	ID   int    `json:"id" xml:"id"`
	Name string `json:"name" xml:"name"`
	Age  int    `json:"age" xml:"age"`
}

func main() {
	server := gserver.NewServer()

	// JSON endpoint
	server.POST("/user", func(ctx *gserver.Context) {
		var user User
		if err := ctx.ShouldBindJSON(&user); err != nil {
			ctx.JSON(400, map[string]string{"error": err.Error()})
			return
		}
		ctx.JSON(200, user)
	})

	// XML endpoint
	server.POST("/user/xml", func(ctx *gserver.Context) {
		var user User
		if err := ctx.ShouldBindXML(&user); err != nil {
			ctx.XML(400, map[string]string{"error": err.Error()})
			return
		}
		ctx.XML(200, user)
	})

	// Query parameter example
	server.GET("/search", func(ctx *gserver.Context) {
		query := ctx.Query("q")
		page := ctx.QueryDefault("page", "1")
		ctx.String(200, "Query: %s, Page: %s", query, page)
	})

	// Form data example
	server.POST("/form", func(ctx *gserver.Context) {
		name := ctx.PostForm("name")
		email := ctx.PostFormDefault("email", "unknown@example.com")
		ctx.String(200, "Name: %s, Email: %s", name, email)
	})

	// Path parameter example
	server.GET("/user/:id", func(ctx *gserver.Context) {
		id := ctx.Param("id")
		ctx.JSON(200, map[string]string{"id": id, "name": "User " + id})
	})

	err := server.Run(":8080")
	if err != nil {
		panic(err)
	}
}
