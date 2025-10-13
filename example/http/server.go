package main

import (
	"gk/ghttp/gserver"
	"log"
	"net"
)

func main() {
	server := gserver.NewServer()
	router := server.Router()
	router.GET("/", func(c *gserver.Context) {

	})
	listen, err := net.Listen("tcp", ":8080")
	if err != nil {
		log.Fatal(err)
	}
	err = server.Serve(listen)
	if err != nil {
		log.Fatal(err)
	}
}
