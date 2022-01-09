package models

type Error struct {
	Err error `json:"error"`
}

func NewError(err error) Error {
	return Error{Err: err}
}

func (e Error) Error() string {
	return string(e.Bytes())
}

func (e Error) Bytes() []byte {
	b := append([]byte{}, `{"error":"`...)
	b = append(b, e.Err.Error()...)
	b = append(b, `"}`...)

	return b
}
