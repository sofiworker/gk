package gserver

import (
	"fmt"
	"reflect"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

// mockMatcher is a mock implementation of the Match interface to record added routes.	ype mockMatcher struct {
type mockMatcher struct {
	routes map[string]string
}

func newMockMatcher() *mockMatcher {
	return &mockMatcher{routes: make(map[string]string)}
}

func (m *mockMatcher) AddRoute(method, path string, handlers ...HandlerFunc) error {
	key := method + "-" + path
	var handlerNames string
	for _, h := range handlers {
		name := runtime.FuncForPC(reflect.ValueOf(h).Pointer()).Name()
		handlerNames += name + ";"
	}
	m.routes[key] = handlerNames
	return nil
}

func (m *mockMatcher) Match(method, path string) *MatchResult {
	// Not needed for these tests
	return nil
}

// Helper to create a new server with a mock matcher for testing.
func newTestServer() (*Server, *mockMatcher) {
	m := newMockMatcher()
	opts := []ServerOption{}
	s := NewServer(opts...)
	return s, m
}

// Dummy handlers for testing middleware ordering.
func middleware1(c *Context) {}
func middleware2(c *Context) {}
func handler1(c *Context)    {}

func TestGroup_Path(t *testing.T) {
	s, m := newTestServer()

	s.GET("/base", handler1)
	assert.Contains(t, m.routes, "GET-/base")

	v1 := s.Group("/v1")
	v1.GET("/users", handler1)
	assert.Contains(t, m.routes, "GET-/v1/users")

	v2 := v1.Group("/v2")
	v2.POST("/items", handler1)
	assert.Contains(t, m.routes, "POST-/v1/v2/items")

	// Test empty relative path
	group := s.Group("/api")
	subgroup := group.Group("")
	subgroup.GET("/endpoint", handler1)
	assert.Contains(t, m.routes, "GET-/api/endpoint")
}

func TestGroup_Middleware(t *testing.T) {
	s, m := newTestServer()
	s.Use(middleware1)

	v1 := s.Group("/v1", middleware2)
	v1.GET("/action", handler1)

	// Build expected handler name string
	m1Name := runtime.FuncForPC(reflect.ValueOf(middleware1).Pointer()).Name()
	m2Name := runtime.FuncForPC(reflect.ValueOf(middleware2).Pointer()).Name()
	h1Name := runtime.FuncForPC(reflect.ValueOf(handler1).Pointer()).Name()
	expected := fmt.Sprintf("%s;%s;%s;", m1Name, m2Name, h1Name)

	assert.Equal(t, expected, m.routes["GET-/v1/action"])
}

func TestGroup_Any(t *testing.T) {
	s, m := newTestServer()
	s.ANY("/any", handler1)

	for _, method := range anyMethods {
		key := method + "-/any"
		assert.Contains(t, m.routes, key)
	}
}

//func TestGroup_Match(t *testing.T) {
//	s, m := newTestServer()
//	methods := []string{http.MethodGet, http.MethodPost}
//	s.Lookup(methods, "/match", handler1)
//
//	for _, method := range methods {
//		key := method + "-/match"
//		assert.Contains(t, m.routes, key)
//	}
//
//	// Ensure other methods are not registered
//	assert.NotContains(t, m.routes, "PUT-/match")
//}

func TestGroup_Chain(t *testing.T) {
	s, m := newTestServer()

	// Test chaining of Group, Use, and a method
	s.Group("/api").Use(middleware1).Group("/v1").GET("/health", handler1)

	m1Name := runtime.FuncForPC(reflect.ValueOf(middleware1).Pointer()).Name()
	h1Name := runtime.FuncForPC(reflect.ValueOf(handler1).Pointer()).Name()
	expected := fmt.Sprintf("%s;%s;", m1Name, h1Name)

	assert.Equal(t, expected, m.routes["GET-/api/v1/health"])
}
