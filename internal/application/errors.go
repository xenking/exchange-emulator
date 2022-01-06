package application

import "github.com/segmentio/encoding/json"

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
	b, _ := json.Marshal(&e)

	return b
}
