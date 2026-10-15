package twelvedata

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type responseError struct {
	status int
	delay  time.Duration
}

func (e responseError) Error() string             { return fmt.Sprintf("Twelve Data HTTP %d", e.status) }
func (e responseError) Temporary() bool           { return e.status == 429 || e.status >= 500 }
func (e responseError) RetryDelay() time.Duration { return e.delay }
func providerResponseError(status int, retryAfter string) responseError {
	delay := time.Duration(0)
	if status == 429 {
		delay = time.Minute
		if n, e := strconv.Atoi(retryAfter); e == nil && n > 0 && n <= 86400 {
			delay = time.Duration(n) * time.Second
		} else if at, e := http.ParseTime(retryAfter); e == nil && time.Until(at) > 0 {
			delay = min(24*time.Hour, time.Until(at))
		}
	}
	return responseError{status, delay}
}
