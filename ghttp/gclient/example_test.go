package gclient

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
)

func ExampleClient_endpoint() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"name": "gk",
			},
		})
	}))
	defer srv.Close()

	unwrapper, checker := JSONEnvelopeHandlers(JSONEnvelopeConfig{})

	client := NewClient().
		SetResponseUnwrapper(unwrapper).
		SetResponseStatusChecker(checker)

	type user struct {
		Name string `json:"name"`
	}

	var out user
	ep := client.NewEndpoint(
		http.MethodGet,
		srv.URL+"/users/{id}",
		WithPathParam("id", "1"),
	)

	resp, err := ep.Execute(WithResult(&out))
	if err != nil {
		panic(err)
	}
	if err := resp.OK(); err != nil {
		panic(err)
	}

	fmt.Println(out.Name)
	// Output:
	// gk
}
