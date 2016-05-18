package gosocketio

import (
	"errors"
	"reflect"
)

type caller struct {
	Func reflect.Value
	Args reflect.Type
	Out  bool
}

var (
	ErrorCallerNotFunc     = errors.New("f is not function")
	ErrorCallerNot2Args    = errors.New("f should have 2 args")
	ErrorCallerMaxOneValue = errors.New("f should return not more than one value")
)

/**
Parses function passed by using reflection, and stores its representation
for further call on message or ack
*/
func newCaller(f interface{}) (*caller, error) {
	fVal := reflect.ValueOf(f)
	if fVal.Kind() != reflect.Func {
		return nil, ErrorCallerNotFunc
	}

	fType := fVal.Type()
	if fType.NumIn() != 2 {
		return nil, ErrorCallerNot2Args
	}

	if fType.NumOut() > 1 {
		return nil, ErrorCallerMaxOneValue
	}

	return &caller{
		Func: fVal,
		Args: fType.In(1),
		Out:  fType.NumOut() == 1,
	}, nil
}

/**
returns function parameter as it is present in it using reflection
*/
func (c *caller) getArgs() interface{} {
	return reflect.New(c.Args).Interface()
}

/**
calls function with given arguments from its representation using reflection
*/
func (c *caller) callFunc(h *Channel, args interface{}) []reflect.Value {
	a := []reflect.Value{reflect.ValueOf(h), reflect.ValueOf(args).Elem()}

	return c.Func.Call(a)
}
