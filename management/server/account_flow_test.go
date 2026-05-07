package server

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/netbirdio/netbird/management/server/types"
)

func TestFlowSettingsChanged(t *testing.T) {
	t.Run("no change", func(t *testing.T) {
		assert.False(t, flowSettingsChanged(
			&types.ExtraSettings{
				FlowEnabled:              true,
				FlowGroups:               []string{"group-1"},
				FlowPacketCounterEnabled: true,
				FlowENCollectionEnabled:  true,
				FlowDnsCollectionEnabled: true,
			},
			&types.ExtraSettings{
				FlowEnabled:              true,
				FlowGroups:               []string{"group-1"},
				FlowPacketCounterEnabled: true,
				FlowENCollectionEnabled:  true,
				FlowDnsCollectionEnabled: true,
			},
		))
	})

	t.Run("detect change", func(t *testing.T) {
		assert.True(t, flowSettingsChanged(
			&types.ExtraSettings{
				FlowEnabled: true,
			},
			&types.ExtraSettings{
				FlowEnabled:              true,
				FlowDnsCollectionEnabled: true,
			},
		))
	})
}
