package flam

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

var (
	// LogRestMwJSONDecorate @todo doc
	LogRestMwJSONDecorate = EnvBool("FLAMINGO_LOG_REST_MW_JSON_DECORATE", true)
)

// LogRestMwAdapter @todo doc
type LogRestMwAdapter interface {
	Request(ctx ...Bag) error
	Response(ctx ...Bag) error
}

type logRestMwAdapter struct {
	logger Logger
}

var _ LogRestMwAdapter = &logRestMwAdapter{}

func newLogRestMwLogAdapter(logger Logger) LogRestMwAdapter {
	return &logRestMwAdapter{
		logger: logger,
	}
}

func (l *logRestMwAdapter) Request(ctx ...Bag) error {
	return l.logger.Signal(LogInfo, "rest", "request", ctx...)
}

func (l *logRestMwAdapter) Response(ctx ...Bag) error {
	return l.logger.Signal(LogInfo, "rest", "response", ctx...)
}

func getLogRestMwRequestHeaders(r *http.Request) Bag {
	headers := Bag{}
	for index, header := range r.Header {
		if len(header) == 1 {
			headers[index] = header[0]
		} else {
			headers[index] = header
		}
	}
	return headers
}

func getLogRestMwRequestParams(r *http.Request) Bag {
	params := Bag{}
	for p, v := range r.URL.Query() {
		if len(v) == 1 {
			params[p] = v[0]
		} else {
			params[p] = v
		}
	}
	return params
}

func getLogRestMwResponseHeaders(r http.ResponseWriter) Bag {
	headers := Bag{}
	for index, header := range r.Header() {
		if len(header) == 1 {
			headers[index] = header[0]
		} else {
			headers[index] = header
		}
	}
	return headers
}

func getLogRestMwRequestBody(r *http.Request) string {
	var bodyBytes []byte
	if r.Body != nil {
		bodyBytes, _ = io.ReadAll(r.Body)
	}
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	return string(bodyBytes)
}

type logRestMwResponseWriter struct {
	http.ResponseWriter
	status int
	body   *bytes.Buffer
}

var _ http.ResponseWriter = &logRestMwResponseWriter{}

func (w *logRestMwResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *logRestMwResponseWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w *logRestMwResponseWriter) Body() []byte {
	return w.body.Bytes()
}

// LogRestMwRequestReader @todo doc
type LogRestMwRequestReader func(r *http.Request) Bag

func newLogRestMwRequestReader() LogRestMwRequestReader {
	reader := func(r *http.Request) Bag {
		return Bag{
			"headers": getLogRestMwRequestHeaders(r),
			"method":  r.Method,
			"path":    r.URL.Path,
			"params":  getLogRestMwRequestParams(r),
			"body":    getLogRestMwRequestBody(r),
		}
	}

	if LogRestMwJSONDecorate {
		return func(r *http.Request) Bag {
			data := reader(r)
			contentType := strings.ToLower(r.Header.Get("Content-Type"))
			if strings.HasPrefix(contentType, "application/json") {
				var model interface{}
				if json.Unmarshal([]byte(data["body"].(string)), &model) == nil {
					data["bodyJson"] = model
				}
			}
			return data
		}
	}

	return reader
}

// LogRestMwResponseReader @todo doc
type LogRestMwResponseReader func(w http.ResponseWriter, r *http.Request, statusCode int) Bag

func newLogRestMwResponseReader() LogRestMwResponseReader {
	reader := func(writer http.ResponseWriter, _ *http.Request, statusCode int) Bag {
		data := Bag{"headers": getLogRestMwResponseHeaders(writer)}
		if w, ok := writer.(*logRestMwResponseWriter); ok {
			data["status"] = w.status
			if w.status != statusCode {
				data["body"] = string(w.Body())
			}
		}
		return data
	}

	if LogRestMwJSONDecorate {
		return func(w http.ResponseWriter, r *http.Request, statusCode int) Bag {
			data := reader(w, r, statusCode)
			if body, ok := data["body"]; ok == true {
				accept := strings.ToLower(r.Header.Get("Accept"))
				if accept == "*/*" || strings.Contains(accept, "application/json") {
					var model interface{}
					if json.Unmarshal([]byte(body.(string)), &model) == nil {
						data["bodyJson"] = model
					}
				}
			}
			return data
		}
	}

	return reader
}

// LogRestMwGenerator @todo doc
type LogRestMwGenerator func(statusCode int) func(http.Handler) http.Handler

func newLogRestMwGenerator(logger LogRestMwAdapter, requestReader LogRestMwRequestReader, responseReader LogRestMwResponseReader) LogRestMwGenerator {
	return func(statusCode int) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				req := requestReader(r)
				_ = logger.Request(Bag{"request": req})

				writer := &logRestMwResponseWriter{
					ResponseWriter: w,
					status:         200,
					body:           &bytes.Buffer{},
				}

				startTimestamp := time.Now().UnixMilli()
				if next != nil {
					next.ServeHTTP(writer, r)
				}
				duration := time.Now().UnixMilli() - startTimestamp

				resp := responseReader(writer, r, statusCode)
				_ = logger.Response(Bag{"request": req, "response": resp, "duration": duration})
			})
		}
	}
}

type logRestMwProvider struct{}

var _ Provider = &logRestMwProvider{}

func newLogRestMwProvider() Provider {
	return &logRestMwProvider{}
}

func (*logRestMwProvider) ID() string {
	return "flam.rest.mw.log"
}

func (p *logRestMwProvider) Reg(app App) error {
	_ = app.DI().Provide(newLogRestMwLogAdapter)
	_ = app.DI().Provide(newLogRestMwRequestReader)
	_ = app.DI().Provide(newLogRestMwResponseReader)
	_ = app.DI().Provide(newLogRestMwGenerator)
	return nil
}
