package flam

import (
	"errors"
	"fmt"
)

// Error @todo doc
type Error interface {
	error

	Unwrap() error
	GetCode() int
	SetCode(code int) Error
	Context() *Bag
	Set(path string, value interface{}) Error
	Get(path string, def interface{}) interface{}
}

// NewError @todo doc
func NewError(msg string, ctx ...Bag) Error {
	context := Bag{}
	for _, c := range ctx {
		context.Merge(c)
	}
	return &err{
		base:    errors.New(msg),
		context: context,
	}
}

// NewErrorFrom @todo doc
func NewErrorFrom(e error, msg string, ctx ...Bag) Error {
	context := Bag{}
	for _, c := range ctx {
		context.Merge(c)
	}
	return &err{
		base:    fmt.Errorf("%s : %w", msg, e),
		context: context,
	}
}

type err struct {
	base    error
	code    int
	context Bag
}

var _ error = &err{}
var _ Error = &err{}

func (e *err) Error() string {
	return e.base.Error()
}

func (e *err) Unwrap() error {
	return errors.Unwrap(e.base)
}

func (e *err) GetCode() int {
	return e.code
}

func (e *err) SetCode(code int) Error {
	e.code = code
	return e
}

func (e *err) Context() *Bag {
	return &e.context
}

func (e *err) Set(path string, value interface{}) Error {
	_ = e.context.Set(path, value)
	return e
}

func (e *err) Get(path string, def interface{}) interface{} {
	return e.context.Get(path, def)
}
