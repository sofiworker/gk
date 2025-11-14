package gerr

type HttpErr struct {
	Code int
	Msg  string
	Err  error
}
