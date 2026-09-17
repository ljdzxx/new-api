package common

import (
	"time"

	"github.com/QuantumNous/new-api/constant"
)

// StreamingTimeout is the maximum wait for response headers or the next stream event.
func StreamingTimeout() time.Duration {
	if constant.StreamingTimeout <= 0 {
		return 30 * time.Second
	}
	return time.Duration(constant.StreamingTimeout) * time.Second
}
