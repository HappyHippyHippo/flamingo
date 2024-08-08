package flam

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// EnvelopeWriter @todo doc
type EnvelopeWriter interface {
	WriteEnvelope(env Envelope) error
}

// EnvelopeError @todo doc
type EnvelopeError interface {
	GetStatus() int
	SetStatus(val int) EnvelopeError
	SetServiceID(val int) EnvelopeError
	SetEndpointID(val int) EnvelopeError
	SetParamID(param int) EnvelopeError
	SetErrorID(id any) EnvelopeError
	GetID() string
	GetContext(key string) interface{}
	SetContext(key string, value interface{}) EnvelopeError
}

// NewEnvelopeError @todo doc
func NewEnvelopeError(status int, e error, ctx ...Bag) EnvelopeError {
	context := Bag{"message": e.Error()}
	for _, c := range ctx {
		context.Merge(c)
	}
	return (&envelopeError{
		status:  status,
		context: context,
	}).compose()
}

type envelopeError struct {
	status     int
	id         string
	serviceID  int
	endpointID int
	paramID    int
	errorID    string
	context    Bag
}

var _ EnvelopeError = &envelopeError{}

func (e *envelopeError) GetStatus() int {
	return e.status
}

func (e *envelopeError) SetStatus(val int) EnvelopeError {
	e.status = val
	return e
}

func (e *envelopeError) SetServiceID(val int) EnvelopeError {
	e.serviceID = val
	return e.compose()
}

func (e *envelopeError) SetEndpointID(val int) EnvelopeError {
	e.endpointID = val
	return e.compose()
}

func (e *envelopeError) SetParamID(param int) EnvelopeError {
	e.paramID = param
	return e.compose()
}

func (e *envelopeError) SetErrorID(id any) EnvelopeError {
	e.errorID = fmt.Sprintf("%v", id)
	return e.compose()
}

func (e *envelopeError) GetID() string {
	return e.id
}

func (e *envelopeError) GetContext(key string) interface{} {
	return e.context[key]
}

func (e *envelopeError) SetContext(key string, value interface{}) EnvelopeError {
	e.context[key] = value
	return e
}

func (e *envelopeError) compose() *envelopeError {
	cb := strings.Builder{}
	if e.serviceID != 0 {
		cb.WriteString(fmt.Sprintf("s:%d", e.serviceID))
	}
	if e.endpointID != 0 {
		if cb.Len() != 0 {
			cb.WriteString(".")
		}
		cb.WriteString(fmt.Sprintf("e:%d", e.endpointID))
	}
	if e.paramID != 0 {
		if cb.Len() != 0 {
			cb.WriteString(".")
		}
		cb.WriteString(fmt.Sprintf("p:%d", e.paramID))
	}
	if e.errorID != "" {
		if cb.Len() != 0 {
			cb.WriteString(".")
		}
		if i, ee := strconv.Atoi(e.errorID); ee != nil {
			cb.WriteString(e.errorID)
		} else {
			cb.WriteString(fmt.Sprintf("c:%d", i))
		}
	}
	e.id = cb.String()
	return e
}

type envelopeListReport interface{}

// PageListReport @todo doc
type PageListReport struct {
	Search string `json:"search,omitempty"`
	Start  int64  `json:"start"`
	Count  int64  `json:"count"`
	Total  int64  `json:"total"`
	Prev   string `json:"prev,omitempty"`
	Next   string `json:"next,omitempty"`
}

func newEnvelopePageListReport(search string, start, count, total int64) envelopeListReport {
	report := &PageListReport{
		Search: search,
		Start:  start,
		Count:  count,
		Total:  total,
		Prev:   "",
		Next:   "",
	}
	if start > 0 {
		nStart := int64(0)
		if count < start {
			nStart = start - count
		}
		report.Prev = fmt.Sprintf("?search=%s&start=%d&count=%d", search, nStart, count)
	}
	if start+count < total {
		report.Next = fmt.Sprintf("?search=%s&start=%d&count=%d", search, start+count, count)
	}
	return report
}

// Envelope @todo doc
type Envelope interface {
	SetServiceID(val int) Envelope
	SetEndpointID(val int) Envelope
	AddError(status int, e error, ctx ...Bag) EnvelopeError
	SetData(id string, data interface{}) Envelope
	SetPageListReport(search string, start, count, total int64) Envelope
}

// NewEnvelope @todo doc
func NewEnvelope(data ...interface{}) Envelope {
	env := &envelope{}
	if len(data) > 0 {
		env.Data = data[0]
	}
	return env
}

type envelope struct {
	Errors []EnvelopeError `json:"errors,omitempty"`
	Data   interface{}     `json:"data,omitempty"`
	Filter interface{}     `json:"filter,omitempty"`
}

func (env *envelope) SetServiceID(val int) Envelope {
	for _, e := range env.Errors {
		e.SetServiceID(val)
	}
	return env
}

func (env *envelope) SetEndpointID(val int) Envelope {
	for _, e := range env.Errors {
		e.SetEndpointID(val)
	}
	return env
}

func (env *envelope) AddError(status int, e error, ctx ...Bag) EnvelopeError {
	envErr := NewEnvelopeError(status, e, ctx...)
	env.Errors = append(env.Errors, envErr)
	return envErr
}

func (env *envelope) SetData(id string, data interface{}) Envelope {
	if _, ok := env.Data.(*Bag); !ok {
		env.Data = &Bag{}
	}
	env.Data.(*Bag).Merge(Bag{id: data})
	return env
}

func (env *envelope) SetPageListReport(search string, start, count, total int64) Envelope {
	env.Filter = newEnvelopePageListReport(search, start, count, total)
	return env
}

// ProblemEnvelope @todo doc
type ProblemEnvelope interface {
	GetStatus() int
	GetID() string
	GetType() string
	GetTitle() string
	GetDetail() string
	GetInstance() string
	Get(key string) interface{}
	Set(key string, value interface{}) ProblemEnvelope
}

// NewProblemEnvelope @todo doc
func NewProblemEnvelope(status int, typeURI, title, details, instance string, ctx ...Bag) ProblemEnvelope {
	pe := &problemEnvelope{}
	for _, c := range ctx {
		for k, v := range c {
			pe.Set(k, v)
		}
	}
	pe.Set("status", status)
	pe.Set("type", typeURI)
	pe.Set("title", title)
	pe.Set("detail", details)
	pe.Set("instance", instance)
	return pe
}

// NewProblemEnvelopeFrom @todo doc
func NewProblemEnvelopeFrom(env Envelope) ProblemEnvelope {
	pe := problemEnvelope{
		"status":  http.StatusInternalServerError,
		"message": "unknown error",
	}

	if len(env.(*envelope).Errors) > 0 {
		e := env.(*envelope).Errors[0]
		for k, v := range e.(*envelopeError).context {
			pe[k] = v
		}
		pe["status"] = e.GetStatus()
		pe["id"] = e.GetID()
	}

	status := pe["status"].(int)
	pe["type"] = fmt.Sprintf("https://developer.mozilla.org/en-US/docs/Web/HTTP/Status/%d", status)
	pe["title"] = http.StatusText(status)
	pe["detail"] = pe["message"]

	return &pe
}

type problemEnvelope Bag

func (pe *problemEnvelope) GetStatus() int {
	v, ok := pe.Get("status").(int)
	if !ok {
		return 0
	}
	return v
}

func (pe *problemEnvelope) GetID() string {
	v, ok := pe.Get("id").(string)
	if !ok {
		return ""
	}
	return v
}

func (pe *problemEnvelope) GetType() string {
	v, ok := pe.Get("type").(string)
	if !ok {
		return ""
	}
	return v
}

func (pe *problemEnvelope) GetTitle() string {
	v, ok := pe.Get("title").(string)
	if !ok {
		return ""
	}
	return v
}

func (pe *problemEnvelope) GetDetail() string {
	v, ok := pe.Get("detail").(string)
	if !ok {
		return ""
	}
	return v
}

func (pe *problemEnvelope) GetInstance() string {
	v, ok := pe.Get("instance").(string)
	if !ok {
		return ""
	}
	return v
}

func (pe *problemEnvelope) Get(key string) interface{} {
	return (*pe)[key]
}

func (pe *problemEnvelope) Set(key string, value interface{}) ProblemEnvelope {
	(*pe)[key] = value
	return pe
}
