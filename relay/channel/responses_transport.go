package channel

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"net/http"
)

const ResponsesTransportContextKey = "responses_websocket_transport"

// ResponsesTransport replaces only the native transport. Request validation,
// conversion, billing, usage accounting and response handling remain shared.
type ResponsesTransport interface {
	RoundTrip(*gin.Context, *relaycommon.RelayInfo, *http.Request) (*http.Response, error)
}
