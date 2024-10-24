package flam

import (
	"reflect"
	"strings"
)

// Bag @todo doc
type Bag map[string]interface{}

// Clone @todo doc
func (b *Bag) Clone() *Bag {
	var cloner func(value interface{}) interface{}
	cloner = func(value interface{}) interface{} {
		switch typedValue := value.(type) {
		case []interface{}:
			var result []interface{}
			for _, i := range typedValue {
				result = append(result, cloner(i))
			}
			return result
		case Bag:
			return *typedValue.Clone()
		case *Bag:
			return *typedValue.Clone()
		default:
			return value
		}
	}
	target := Bag{}
	for key, value := range *b {
		target[key] = cloner(value)
	}
	return &target
}

// Entries @todo doc
func (b *Bag) Entries() []string {
	var entries []string
	for key := range *b {
		entries = append(entries, key)
	}
	return entries
}

// Has @todo doc
func (b *Bag) Has(path string) bool {
	_, e := b.path(path)
	return e == nil
}

// Set @todo doc
func (b *Bag) Set(path string, value interface{}) error {
	parts := strings.Split(path, ".")
	it := b
	if len(parts) == 1 {
		(*it)[path] = value
		return nil
	}
	generate := func(part string) {
		generate := false
		if next, ok := (*it)[part]; !ok {
			generate = true
		} else if _, ok := next.(Bag); !ok {
			generate = true
		}
		if generate == true {
			(*it)[part] = Bag{}
		}
	}
	for _, part := range parts[:len(parts)-1] {
		if part == "" {
			continue
		}
		generate(part)
		next := (*it)[part].(Bag)
		it = &next
	}
	part := parts[len(parts)-1:][0]
	generate(part)
	(*it)[part] = value

	return nil
}

// Get @todo doc
func (b *Bag) Get(path string, def interface{}) interface{} {
	val, e := b.path(path)
	if e != nil {
		return def
	}
	return val
}

// Bool @todo doc
func (b *Bag) Bool(path string, def bool) bool {
	v := b.Get(path, def)
	if typedValue, ok := v.(bool); ok {
		return typedValue
	}
	return def
}

// Int @todo doc
func (b *Bag) Int(path string, def int) int {
	v := b.Get(path, def)
	if typedValue, ok := v.(int); ok {
		return typedValue
	}
	return def
}

// Float @todo doc
func (b *Bag) Float(path string, def float64) float64 {
	v := b.Get(path, def)
	if typedValue, ok := v.(float64); ok {
		return typedValue
	}
	return def
}

// String @todo doc
func (b *Bag) String(path, def string) string {
	v := b.Get(path, def)
	if typedValue, ok := v.(string); ok {
		return typedValue
	}
	return def
}

// List @todo doc
func (b *Bag) List(path string, def []interface{}) []interface{} {
	v := b.Get(path, def)
	if typedValue, ok := v.([]interface{}); ok {
		return typedValue
	}
	return def
}

// Bag @todo doc
func (b *Bag) Bag(path string, def *Bag) *Bag {
	v := b.Get(path, def)
	switch typedValue := v.(type) {
	case Bag:
		return &typedValue
	case *Bag:
		return typedValue
	}
	return def
}

// Merge @todo doc
func (b *Bag) Merge(src Bag) *Bag {
	for _, key := range src.Entries() {
		value := src.Get(key, nil)
		switch tValue := value.(type) {
		case Bag:
			switch tLocal := (*b)[key].(type) {
			case Bag:
				tLocal.Merge(tValue)
			case *Bag:
				tLocal.Merge(tValue)
			default:
				v := Bag{}
				v.Merge(tValue)
				(*b)[key] = v
			}
		default:
			(*b)[key] = value
		}
	}
	return b
}

// Populate @todo doc
func (b *Bag) Populate(path string, target interface{}, insensitive ...bool) error {
	if reflect.TypeOf(target).Kind() != reflect.Ptr {
		return NewError("non-pointer target", Bag{"target": target})
	}
	isInsensitive := false
	if len(insensitive) == 0 || insensitive[0] == true {
		isInsensitive = true
		path = strings.ToLower(path)
	}
	source := b.Get(path, nil)
	if source == nil {
		return NewError("invalid path", Bag{"path": path})
	}
	return b.populate(reflect.ValueOf(source), reflect.ValueOf(target).Elem(), isInsensitive)
}

func (b *Bag) populate(source, target reflect.Value, insensitive bool) (e error) {
	switch target.Kind() {
	case reflect.Struct:
		if source.Kind() != reflect.Map {
			return NewError("conversion error", Bag{"Bag": source})
		}
		for i := 0; i < target.NumField(); i++ {
			typeOfField := target.Type().Field(i)
			if !typeOfField.IsExported() {
				continue
			}
			path := typeOfField.Name
			if insensitive {
				path = strings.ToLower(path)
			}
			s := source.MapIndex(reflect.ValueOf(path))
			if s.Kind() == reflect.Invalid {
				continue
			}
			if s.Kind() == reflect.Interface {
				s = s.Elem()
			}
			field := target.Field(i)
			if e := b.populate(s, field, insensitive); e != nil {
				return e
			}
		}
	default:
		switch source.Kind() {
		case reflect.Invalid:
			return nil
		case reflect.Pointer:
			if !source.CanConvert(target.Type()) {
				return NewError("conversion error", Bag{target.Kind().String(): source})
			}
			source = source.Convert(target.Type())
		case target.Kind():
		default:
			return NewError("conversion error", Bag{target.Kind().String(): source})
		}
		target.Set(source)
		return nil
	}
	return nil
}

func (b *Bag) path(path string) (interface{}, error) {
	var ok bool
	var it interface{}

	it = *b
	for _, part := range strings.Split(path, ".") {
		if part == "" {
			continue
		}
		switch typedIt := it.(type) {
		case Bag:
			if it, ok = typedIt[part]; !ok {
				return nil, NewError("invalid bag path", Bag{"path": path})
			}
		case *Bag:
			if it, ok = (*typedIt)[part]; !ok {
				return nil, NewError("invalid bag path", Bag{"path": path})
			}
		default:
			return nil, NewError("invalid bag path", Bag{"path": path})
		}
	}
	return it, nil
}
