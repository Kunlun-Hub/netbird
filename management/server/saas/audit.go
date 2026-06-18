package saas

import (
	"context"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/netbirdio/netbird/management/server/activity"
)

// AuditRecorder stores SaaS operation events in the shared activity log.
type AuditRecorder struct {
	Store activity.Store
}

func (r AuditRecorder) Record(ctx context.Context, initiatorID, targetID, accountID string, activityID activity.Activity, meta map[string]any) {
	if r.Store == nil {
		return
	}
	if meta == nil {
		meta = map[string]any{}
	}
	if _, err := r.Store.Save(ctx, &activity.Event{
		Timestamp:   time.Now().UTC(),
		Activity:    activityID,
		InitiatorID: initiatorID,
		TargetID:    targetID,
		AccountID:   accountID,
		Meta:        meta,
	}); err != nil {
		log.WithContext(ctx).Warnf("failed to store SaaS audit event %s for account %s: %v", activityID.StringCode(), accountID, err)
	}
}
