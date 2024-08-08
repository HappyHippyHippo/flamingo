package test

import (
	"fmt"
	"net/http"
)

// ErrorResponse @todo doc
type ErrorResponse struct {
	Detail  string `json:"detail"`
	Id      string `json:"id"`
	Message string `json:"message"`
	Status  int    `json:"status"`
	Title   string `json:"title"`
	Type    string `json:"type"`
}

// GetErrorResponse @todo doc
func GetErrorResponse(id, message string, status int) ErrorResponse {
	return ErrorResponse{
		Id:      id,
		Detail:  message,
		Message: message,
		Status:  status,
		Title:   http.StatusText(status),
		Type:    fmt.Sprintf("https://developer.mozilla.org/en-US/docs/Web/HTTP/Status/%d", status),
	}
}
