// Package webbench benchmarks ghttp against popular Go web frameworks with a
// shared set of scenarios. All frameworks register the exact same routes and
// implement handlers with the same semantics, so results are comparable.
package webbench

// userIn is the canonical JSON-bind request payload.
type userIn struct {
	Name   string   `json:"name"`
	Email  string   `json:"email"`
	Age    int      `json:"age"`
	Active bool     `json:"active"`
	Tags   []string `json:"tags"`
}

// userOut is the canonical JSON-bind response payload.
type userOut struct {
	ID     int64    `json:"id"`
	Name   string   `json:"name"`
	Email  string   `json:"email"`
	Age    int      `json:"age"`
	Active bool     `json:"active"`
	Tags   []string `json:"tags"`
}

func makeUserOut(in userIn) userOut {
	return userOut{
		ID:     1,
		Name:   in.Name,
		Email:  in.Email,
		Age:    in.Age,
		Active: in.Active,
		Tags:   in.Tags,
	}
}

// orderItem / orderIn / orderOut model the full-chain scenario:
// path param + query params + headers + JSON body in, JSON out.
type orderItem struct {
	SKU   string  `json:"sku"`
	Qty   int     `json:"qty"`
	Price float64 `json:"price"`
}

type orderIn struct {
	Items  []orderItem `json:"items"`
	Note   string      `json:"note"`
	Coupon string      `json:"coupon"`
}

type orderOut struct {
	OrderID   string  `json:"order_id"`
	UserID    string  `json:"user_id"`
	Currency  string  `json:"currency"`
	Expand    string  `json:"expand"`
	RequestID string  `json:"request_id"`
	ItemCount int     `json:"item_count"`
	Total     float64 `json:"total"`
	Note      string  `json:"note"`
}

func makeOrderOut(userID, expand, currency, requestID string, in orderIn) orderOut {
	total := 0.0
	for _, it := range in.Items {
		total += float64(it.Qty) * it.Price
	}
	return orderOut{
		OrderID:   "ord-1",
		UserID:    userID,
		Currency:  currency,
		Expand:    expand,
		RequestID: requestID,
		ItemCount: len(in.Items),
		Total:     total,
		Note:      in.Note,
	}
}

// profile is a medium-size JSON response payload (~12 fields, nested).
type profileAddress struct {
	Country string `json:"country"`
	City    string `json:"city"`
	Street  string `json:"street"`
	Zip     string `json:"zip"`
}

type profile struct {
	ID        int64          `json:"id"`
	Name      string         `json:"name"`
	Email     string         `json:"email"`
	Age       int            `json:"age"`
	Active    bool           `json:"active"`
	Score     float64        `json:"score"`
	Bio       string         `json:"bio"`
	Tags      []string       `json:"tags"`
	Address   profileAddress `json:"address"`
	Followers int            `json:"followers"`
	Following int            `json:"following"`
	CreatedAt string         `json:"created_at"`
}

var profileFixture = profile{
	ID:     42,
	Name:   "Alice",
	Email:  "alice@example.com",
	Age:    30,
	Active: true,
	Score:  99.5,
	Bio:    "Gopher. Building web frameworks and measuring them carefully.",
	Tags:   []string{"go", "http", "benchmark", "web"},
	Address: profileAddress{
		Country: "CN",
		City:    "Shanghai",
		Street:  "1 Example Road",
		Zip:     "200000",
	},
	Followers: 1234,
	Following: 321,
	CreatedAt: "2024-01-02T15:04:05Z",
}

// pingOut is the minimal JSON body used by typed frameworks for /ping.
type pingOut struct {
	Message string `json:"message"`
}

// scaleResourceCount * 4 routes are registered for the route-scale scenario.
const scaleResourceCount = 50
