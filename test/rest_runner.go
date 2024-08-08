package test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	flam "github.com/happyhippyhippo/flamingo"
)

// RestRunner @todo doc
type RestRunner interface {
	Runner

	ExpectStatusCode(status int) RestRunner
	ExpectErrorResponse(id, message string, status int) RestRunner

	RequestInit(method, url string) RestRunner
	RequestBody(body interface{}) RestRunner
	RequestHeader(key, value string) RestRunner
	RequestDo() (*http.Response, error)

	GetResponse() *http.Response
	DecodeResponse(res *http.Response, v interface{}) error
}

type restRunner struct {
	runner

	request struct {
		method  string
		url     string
		body    string
		headers map[string]string
	}

	response struct {
		res *http.Response
		e   error
	}
}

var _ RestRunner = &restRunner{}

// NewRestRunner @todo doc
func NewRestRunner(t *testing.T, app flam.App, configs ...string) (RestRunner, error) {
	r, e := NewRunner(t, app, configs...)
	if e != nil {
		return nil, e
	}
	rr := &restRunner{
		runner: *r.(*runner),
	}
	rr.WithProcess("flam.process.rest")
	return rr, nil
}

func (r *restRunner) ExpectStatusCode(status int) RestRunner {
	r.WithCheck(fmt.Sprintf("should return the expected %d status code", status), func() {
		if r.response.res.StatusCode != status {
			r.t.Errorf("Expected %+v status code, got %+v", status, r.response.res.StatusCode)
		}
	})
	return r
}

func (r *restRunner) ExpectErrorResponse(id, message string, status int) RestRunner {
	r.WithCheck("expect error response", func() {
		expected := GetErrorResponse(id, message, status)
		result := ErrorResponse{}
		_ = r.DecodeResponse(r.response.res, &result)
		if !reflect.DeepEqual(expected, result) {
			r.t.Errorf("Unexpected error response. Expected: %v, Got: %v", expected, result)
		}
	})
	return r
}

func (r *restRunner) RequestInit(method, url string) RestRunner {
	r.request.method = method
	r.request.url = url
	r.request.body = ""
	r.request.headers = nil
	return r
}

func (r *restRunner) RequestBody(body interface{}) RestRunner {
	b, _ := json.Marshal(body)
	r.request.body = string(b)
	return r
}

func (r *restRunner) RequestHeader(key, value string) RestRunner {
	if r.request.headers == nil {
		r.request.headers = map[string]string{}
	}
	r.request.headers[key] = value
	return r
}

func (r *restRunner) RequestDo() (*http.Response, error) {
	client := &http.Client{}
	req, _ := http.NewRequest(r.request.method, r.request.url, strings.NewReader(r.request.body))
	if r.request.headers != nil {
		for k, v := range r.request.headers {
			req.Header.Set(k, v)
		}
	}
	res, e := client.Do(req)
	r.response.res = res
	r.response.e = e

	_ = r.app.DI().Invoke(func(k flam.WatchdogKennel) {
		p, _ := k.GetProcess("flam.process.rest")
		_ = p.Terminate()
	})

	return res, e
}

func (r *restRunner) GetResponse() *http.Response {
	return r.response.res
}

func (r *restRunner) DecodeResponse(res *http.Response, v interface{}) error {
	return json.NewDecoder(res.Body).Decode(&v)
}
