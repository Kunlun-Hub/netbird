package util

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netbirdio/netbird/shared/management/status"
)

func TestWriteErrorIncludesStructuredStatusDetails(t *testing.T) {
	err := status.ErrorfWithDetails(
		status.PermissionDenied,
		"limit_exceeded",
		map[string]interface{}{
			"limit":         "users",
			"current":       4,
			"allowed":       3,
			"required_plan": "pro",
		},
		"limit_exceeded: limit users allows 3, current 4 on basic plan",
	)

	recorder := httptest.NewRecorder()
	WriteError(context.Background(), err, recorder)

	res := recorder.Result()
	defer res.Body.Close()

	require.Equal(t, http.StatusForbidden, recorder.Code)

	var response ErrorResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&response))
	assert.Equal(t, http.StatusForbidden, response.Code)
	assert.Equal(t, "limit_exceeded", response.ErrorCode)
	assert.Equal(t, "users", response.Limit)
	assert.Equal(t, "pro", response.RequiredPlan)
	require.NotNil(t, response.Current)
	assert.Equal(t, 4, *response.Current)
	require.NotNil(t, response.Allowed)
	assert.Equal(t, 3, *response.Allowed)
}
