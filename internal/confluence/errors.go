package confluence

import "fmt"

// APIError is returned by the client when Confluence responds with a 4xx/5xx
// status that is not retryable at the transport layer. Callers can type-assert
// to read StatusCode and branch on specific conditions such as 409 Conflict.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("confluence API error %d: %s", e.StatusCode, e.Body)
}
