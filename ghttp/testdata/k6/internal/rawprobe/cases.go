package rawprobe

type Case struct {
	Name           string
	Payload        []byte
	AllowedStatus  []int
	ExpectClose    bool
	AllowTimeout   bool
	MaxResponseLen int
	Secret         string
	Segments       [][]byte
	SegmentDelayMS int
}
type Result struct {
	Name     string `json:"name"`
	Class    string `json:"class"`
	Status   int    `json:"status"`
	Response []byte `json:"response,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Healthy  bool   `json:"healthy"`
}
type Report struct {
	Results []Result `json:"results"`
	Passed  int      `json:"passed"`
	Failed  int      `json:"failed"`
}

func DefaultCases(secret string, max int) []Case {
	if max <= 0 {
		max = 1 << 20
	}
	get := func(name, path string) Case {
		return Case{Name: name, Payload: []byte("GET " + path + " HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n"), AllowedStatus: []int{200, 400, 404, 431, 500}, ExpectClose: true, MaxResponseLen: max, Secret: secret}
	}
	cs := []Case{get("baseline", "/health"), get("duplicate-content-length", "/"), get("te-cl-conflict", "/"), get("header-injection", "/"), get("invalid-header", "/"), get("nul-url", "/a%00b"), get("long-request", "/"+string(make([]byte, 8192))), get("long-header", "/")}
	cs[1].Payload = []byte("GET / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 0\r\nContent-Length: 1\r\n\r\n")
	cs[2].Payload = []byte("POST / HTTP/1.1\r\nHost: localhost\r\nTransfer-Encoding: chunked\r\nContent-Length: 1\r\n\r\n0\r\n\r\n")
	cs[2].ExpectClose = false
	cs = append(cs, Case{Name: "declared-body-short", Payload: []byte("POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 8\r\n\r\nxy"), AllowedStatus: []int{400, 408}, AllowTimeout: true, MaxResponseLen: max, Secret: secret})
	cs = append(cs, Case{Name: "invalid-chunk", Payload: []byte("POST / HTTP/1.1\r\nHost: localhost\r\nTransfer-Encoding: chunked\r\n\r\nZZ\r\nabc\r\n0\r\n\r\n"), AllowedStatus: []int{400, 404, 408}, ExpectClose: true, MaxResponseLen: max, Secret: secret})
	cs = append(cs, Case{Name: "invalid-trailer", Payload: []byte("POST / HTTP/1.1\r\nHost: localhost\r\nTransfer-Encoding: chunked\r\n\r\n1\r\na\r\n0\r\nBad Trailer\r\n\r\n"), AllowedStatus: []int{400, 404, 408}, ExpectClose: true, MaxResponseLen: max, Secret: secret})
	cs[3].Payload = []byte("GET / HTTP/1.1\r\nHost: localhost\r\nX-Bad: ok\rInjected: 1\r\n\r\n")
	cs[4].Payload = []byte("GET / HTTP/1.1\r\nHost: localhost\r\nBad Header: x\r\n\r\n")
	cs[5].Payload = append([]byte("GET /a"), append([]byte{0}, []byte("b HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")...)...)
	cs[7].Payload = []byte("GET / HTTP/1.1\r\nHost: localhost\r\nX-Long: " + string(make([]byte, 9000)) + "\r\n\r\n")
	cs = append(cs, Case{Name: "header-flood", Payload: []byte("GET / HTTP/1.1\r\n" + string(make([]byte, 10000)) + "\r\n"), AllowedStatus: []int{400, 431}, MaxResponseLen: max, Secret: secret})
	cs = append(cs, get("absolute-uri", "http://example.com/"), get("duplicate-query", "/x?a=1&a=2"), get("slow-segment", "/health"))
	cs[len(cs)-1].Payload = nil
	cs[len(cs)-1].Segments = [][]byte{[]byte("GET /health HTTP/1.1\r\nHost: localhost\r\n"), []byte("Connection: close\r\n\r\n")}
	cs[len(cs)-1].SegmentDelayMS = 1500
	return cs
}
