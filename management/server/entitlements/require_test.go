package entitlements

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netbirdio/netbird/shared/management/status"
)

func TestRequireFeatureDeniedReturnsStructuredStatus(t *testing.T) {
	err := RequireFeature(
		context.Background(),
		NewChecker(NewBasicStaticProvider()),
		"account-a",
		FeatureIdentityProviders,
	)
	require.Error(t, err)

	sErr, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, status.PermissionDenied, sErr.Type())
	assert.Equal(t, "feature_not_entitled", sErr.Code)
	assert.Equal(t, string(FeatureIdentityProviders), sErr.Details["feature"])
	assert.Equal(t, string(PlanPro), sErr.Details["required_plan"])
}

func TestRequireLimitDeniedReturnsStructuredStatus(t *testing.T) {
	err := RequireLimit(
		context.Background(),
		NewChecker(NewBasicStaticProvider()),
		"account-a",
		LimitUsers,
		4,
	)
	require.Error(t, err)

	sErr, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, status.PermissionDenied, sErr.Type())
	assert.Equal(t, "limit_exceeded", sErr.Code)
	assert.Equal(t, string(LimitUsers), sErr.Details["limit"])
	assert.Equal(t, 4, sErr.Details["current"])
	assert.Equal(t, 3, sErr.Details["allowed"])
	assert.Equal(t, string(PlanPro), sErr.Details["required_plan"])
}
