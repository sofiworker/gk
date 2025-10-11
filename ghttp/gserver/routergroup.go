package gserver

type Group struct {
	app         *App
	parentGroup *Group
	name        string

	Prefix          string
	anyRouteDefined bool
}
