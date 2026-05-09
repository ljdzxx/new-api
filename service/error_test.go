package service

import (
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestResetStatusCode(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		statusCode       int
		upstreamCode     int
		statusCodeConfig string
		expectedCode     int
		expectedUpstream int
	}{
		{
			name:             "map string value",
			statusCode:       429,
			statusCodeConfig: `{"429":"503"}`,
			expectedCode:     503,
			expectedUpstream: 429,
		},
		{
			name:             "map int value",
			statusCode:       429,
			statusCodeConfig: `{"429":503}`,
			expectedCode:     503,
			expectedUpstream: 429,
		},
		{
			name:             "skip invalid string value",
			statusCode:       429,
			statusCodeConfig: `{"429":"bad-code"}`,
			expectedCode:     429,
			expectedUpstream: 0,
		},
		{
			name:             "skip status code 200",
			statusCode:       200,
			statusCodeConfig: `{"200":503}`,
			expectedCode:     200,
			expectedUpstream: 0,
		},
		{
			name:             "keep existing upstream code",
			statusCode:       429,
			upstreamCode:     401,
			statusCodeConfig: `{"429":503}`,
			expectedCode:     503,
			expectedUpstream: 401,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			newAPIError := &types.NewAPIError{
				StatusCode:         tc.statusCode,
				UpstreamStatusCode: tc.upstreamCode,
			}
			ResetStatusCode(newAPIError, tc.statusCodeConfig)
			require.Equal(t, tc.expectedCode, newAPIError.StatusCode)
			require.Equal(t, tc.expectedUpstream, newAPIError.UpstreamStatusCode)
		})
	}
}
