package gerr

import "testing"

func TestErrors(t *testing.T) {
	e := Err{}
	_ = e
	
	he := HttpErr{
		Code: 404,
		Msg: "Not Found",
		Err: nil,
	}
	if he.Code != 404 {
		t.Error("bad code")
	}
}
