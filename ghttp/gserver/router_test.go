package gserver

// // 定义数据结构
// type User struct {
// 	ID    int    `json:"id"`
// 	Name  string `json:"name" required:"true" min:"1" max:"50"`
// 	Age   int    `json:"age"`
// 	Email string `json:"email" pattern:"^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"`
// }

// type Post struct {
// 	Title   string `json:"title" required:"true"`
// 	Content string `json:"content"`
// }

// func TestXxx(t *testing.T) {

// 	// 创建路由器
// 	router := ghttp.New()

// 	// 使用标准 HandlerFunc
// 	router.GET("/users/:id", func(c *ghttp.Context) {
// 		id, _ := c.ParamInt("id")
// 		user := &User{
// 			ID:   id,
// 			Name: "John Doe",
// 			Age:  30,
// 		}
// 		c.Success(user)
// 	})

// 	// 使用自定义函数签名（自动处理参数和响应）
// 	router.POST("/users", func(user *User) (*User, error) {
// 		// 处理逻辑
// 		user.ID = 123
// 		return user, nil
// 	})

// 	// 启动服务器
// 	server := &fasthttp.Server{
// 		Handler: router.Serve(),
// 	}
// 	server.ListenAndServe(":8080")
// }
