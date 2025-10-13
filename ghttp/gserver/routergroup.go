package gserver

type RouterGroup struct {
	//Handlers HandlersChain
	basePath string
	engine   *Server
	root     bool
}
