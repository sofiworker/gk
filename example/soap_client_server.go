//go:build ignore
// +build ignore

package main

import (
	"context"
	"log"
	"net/http"

	"github.com/sofiworker/gk/ghttp/gserver"
)

//go:generate go run github.com/sofiworker/gk/cmd/gksoap -wsdl ./contract/user.wsdl -o ./userws -pkg userws

// 该示例演示生成后的典型使用方式。
// `userws` 包需要先通过上面的 go:generate 生成。

type userService struct{}

func main() {
	_ = context.Background()

	// 标准 net/http 服务端
	var _ http.Handler

	// 生成后可直接使用：
	//
	// h, err := userws.NewUserServiceHandler(userService{})
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// log.Fatal(http.ListenAndServe(":8080", h))

	// 也可以挂到 gserver：
	s := gserver.NewServer()
	_ = s
	// if err := userws.RegisterUserServiceServer(s, "/user", userService{}); err != nil {
	// 	log.Fatal(err)
	// }

	// 调用端：
	//
	// client := userws.NewUserServiceClient("http://127.0.0.1:8080/user")
	// req, err := client.NewCreateUserRequest(context.Background(), &userws.CreateUserRequest{Name: "alice"})
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// req.SetHeader("X-Trace-ID", "trace-1")
	// resp, err := client.CreateUser(context.Background(), &userws.CreateUserRequest{Name: "alice"})
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// log.Printf("user id=%s", resp.ID)

	log.Println("run go generate before using this example")
}
