package flam

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type envelopeRestMwResponseWriter struct {
	w          http.ResponseWriter
	serviceID  int
	endpointID int
	status     int
}

var _ http.ResponseWriter = &envelopeRestMwResponseWriter{}
var _ EnvelopeWriter = &envelopeRestMwResponseWriter{}

func (erw *envelopeRestMwResponseWriter) Header() http.Header {
	return erw.w.Header()
}

func (erw *envelopeRestMwResponseWriter) Write(bytes []byte) (int, error) {
	return erw.w.Write(bytes)
}

func (erw *envelopeRestMwResponseWriter) WriteHeader(statusCode int) {
	erw.w.WriteHeader(statusCode)
}

func (erw *envelopeRestMwResponseWriter) WriteEnvelope(env Envelope) error {
	if len(env.(*envelope).Errors) > 0 {
		env.(*envelope).Errors[0].SetServiceID(erw.serviceID).SetEndpointID(erw.endpointID)
		pr := NewProblemEnvelopeFrom(env)
		pr.Set("type", fmt.Sprintf("https://developer.mozilla.org/en-US/docs/Web/HTTP/Status/%d", pr.GetStatus()))
		pr.Set("title", http.StatusText(pr.GetStatus()))
		pr.Set("detail", pr.Get("message"))
		erw.w.Header().Set("Content-Type", "application/problem+json")
		erw.w.WriteHeader(pr.GetStatus())
		return json.NewEncoder(erw.w).Encode(pr)
	}

	erw.w.Header().Set("Content-Type", "application/json")
	erw.w.WriteHeader(erw.status)
	return json.NewEncoder(erw.w).Encode(env)
}

// EnvelopeRestMwGenerator @todo doc
type EnvelopeRestMwGenerator func(serviceID, endpointID, statusCode int) func(http.Handler) http.Handler

func newEnvelopeRestMwGenerator() EnvelopeRestMwGenerator {
	return func(serviceID, endpointID, statusCode int) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writer := &envelopeRestMwResponseWriter{
					w:          w,
					serviceID:  serviceID,
					endpointID: endpointID,
					status:     statusCode,
				}

				parse := func(val interface{}) {
					response := NewEnvelope()

					switch v := val.(type) {
					case Envelope:
						response = v
					case Error:
						ctx := v.Context()
						response.AddError(ctx.Int("status", http.StatusInternalServerError), v, *ctx).SetErrorID(v.GetCode())
					case error:
						response.AddError(http.StatusInternalServerError, v)
					default:
						response.AddError(http.StatusInternalServerError, fmt.Errorf("internal server error"))
					}

					_ = writer.WriteEnvelope(response)
				}

				defer func() {
					if e := recover(); e != nil {
						parse(e)
					}
				}()

				if next != nil {
					next.ServeHTTP(writer, r)
				}
			})
		}
	}
}

type envelopeRestMwProvider struct{}

var _ Provider = &envelopeRestMwProvider{}

func newEnvelopeRestMwProvider() Provider {
	return &envelopeRestMwProvider{}
}

func (*envelopeRestMwProvider) ID() string {
	return "flam.rest.mw.envelope"
}

func (p *envelopeRestMwProvider) Reg(app App) error {
	_ = app.DI().Provide(newEnvelopeRestMwGenerator)
	return nil
}
