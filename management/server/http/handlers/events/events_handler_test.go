package events

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netbirdio/netbird/management/internals/modules/networktraffic"
	nbcontext "github.com/netbirdio/netbird/management/server/context"
	nbstore "github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/shared/auth"

	"github.com/netbirdio/netbird/management/server/activity"
	"github.com/netbirdio/netbird/management/server/mock_server"
	"github.com/netbirdio/netbird/management/server/types"
	"github.com/netbirdio/netbird/shared/management/http/api"
)

func initEventsTestData(account string, events ...*activity.Event) *handler {
	return &handler{
		accountManager: &mock_server.MockAccountManager{
			GetEventsFunc: func(_ context.Context, accountID, userID string) ([]*activity.Event, error) {
				if accountID == account {
					return events, nil
				}
				return []*activity.Event{}, nil
			},
			GetUsersFromAccountFunc: func(_ context.Context, accountID, userID string) (map[string]*types.UserInfo, error) {
				return make(map[string]*types.UserInfo), nil
			},
		},
	}
}

func TestNetworkTrafficSummaryBucketSeconds(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/events/network-traffic/summary?bucket_seconds=30", nil)

	assert.Equal(t, 60, parseBucketSeconds(req))

	req = httptest.NewRequest(http.MethodGet, "/api/events/network-traffic/summary?bucket_seconds=60", nil)

	assert.Equal(t, 60, parseBucketSeconds(req))
}

func TestNetworkTrafficSummaryPointLimit(t *testing.T) {
	endTime := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	startTime := endTime.Add(-15 * 24 * time.Hour)
	filter := networktraffic.Filter{
		StartDate: &startTime,
		EndDate:   &endTime,
	}

	points := countSummaryPoints(filter, networkTrafficSummaryBucketSeconds)
	require.Positive(t, points)
	assert.LessOrEqual(t, points, int64(networkTrafficSummaryMaxPoints))

	tooWideStart := endTime.Add(-16 * 24 * time.Hour)
	filter.StartDate = &tooWideStart

	assert.Greater(t, countSummaryPoints(filter, networkTrafficSummaryBucketSeconds), int64(networkTrafficSummaryMaxPoints))
}

func TestGetAllDNSEventsForcesDNSOnlyFilter(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	accountID := "account-id"
	storeMock := nbstore.NewMockStore(ctrl)
	storeMock.EXPECT().
		GetAccountNetworkTrafficEvents(gomock.Any(), nbstore.LockingStrengthNone, accountID, gomock.AssignableToTypeOf(networktraffic.Filter{})).
		DoAndReturn(func(_ context.Context, _ nbstore.LockingStrength, _ string, filter networktraffic.Filter) ([]*networktraffic.Event, int64, error) {
			require.NotNil(t, filter.DNS)
			require.True(t, *filter.DNS)
			require.NotNil(t, filter.AggregateFlows)
			require.False(t, *filter.AggregateFlows)
			require.NotNil(t, filter.RequireDNSAnswers)
			require.True(t, *filter.RequireDNSAnswers)
			require.Nil(t, filter.InternalDNS)
			require.Equal(t, "example.com", *filter.DNSDomain)

			return []*networktraffic.Event{{
				ID:                 "dns-event",
				AccountID:          accountID,
				FlowID:             "flow-dns",
				Timestamp:          time.Now().UTC(),
				SourceAddress:      "100.80.1.1:53000",
				DestinationAddress: "100.80.1.53:53",
				DNSDomain:          "example.com",
				DNSQueryType:       "A",
				DNSAnswers:         []string{"93.184.216.34"},
				DNSRCode:           "NOERROR",
			}}, 1, nil
		})

	h := &handler{accountManager: &mock_server.MockAccountManager{
		GetStoreFunc: func() nbstore.Store {
			return storeMock
		},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/events/dns?dns=false&aggregate_flows=true&internal_dns=true&dns_domain=example.com", nil)
	req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{AccountId: accountID, UserId: "user-id"})
	recorder := httptest.NewRecorder()

	h.getAllDNSEvents(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Data         []map[string]any `json:"data"`
		TotalRecords int              `json:"total_records"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, 1, response.TotalRecords)
	require.Len(t, response.Data, 1)
	require.Equal(t, "example.com", response.Data[0]["domain"])
	require.Equal(t, "A", response.Data[0]["query_type"])
	require.NotContains(t, response.Data[0], "rx_bytes")
	require.NotContains(t, response.Data[0], "rx_packets")
	require.NotContains(t, response.Data[0], "tx_bytes")
	require.NotContains(t, response.Data[0], "tx_packets")
}

func generateEvents(accountID, userID string) []*activity.Event {
	ID := uint64(1)
	events := make([]*activity.Event, 0)
	events = append(events, &activity.Event{
		Timestamp:   time.Now().UTC(),
		Activity:    activity.PeerAddedByUser,
		ID:          ID,
		InitiatorID: userID,
		TargetID:    "100.64.0.2",
		AccountID:   accountID,
		Meta:        map[string]any{"some": "meta"},
	})
	ID++
	events = append(events, &activity.Event{
		Timestamp:   time.Now().UTC(),
		Activity:    activity.UserJoined,
		ID:          ID,
		InitiatorID: userID,
		TargetID:    "",
		AccountID:   accountID,
		Meta:        map[string]any{"some": "meta"},
	})
	ID++
	events = append(events, &activity.Event{
		Timestamp:   time.Now().UTC(),
		Activity:    activity.GroupCreated,
		ID:          ID,
		InitiatorID: userID,
		TargetID:    "group-id",
		AccountID:   accountID,
		Meta:        map[string]any{"some": "meta"},
	})
	ID++
	events = append(events, &activity.Event{
		Timestamp:   time.Now().UTC(),
		Activity:    activity.SetupKeyUpdated,
		ID:          ID,
		InitiatorID: userID,
		TargetID:    "setup-key-id",
		AccountID:   accountID,
		Meta:        map[string]any{"some": "meta"},
	})
	ID++
	events = append(events, &activity.Event{
		Timestamp:   time.Now().UTC(),
		Activity:    activity.SetupKeyUpdated,
		ID:          ID,
		InitiatorID: userID,
		TargetID:    "setup-key-id",
		AccountID:   accountID,
		Meta:        map[string]any{"some": "meta"},
	})
	ID++
	events = append(events, &activity.Event{
		Timestamp:   time.Now().UTC(),
		Activity:    activity.SetupKeyRevoked,
		ID:          ID,
		InitiatorID: userID,
		TargetID:    "setup-key-id",
		AccountID:   accountID,
		Meta:        map[string]any{"some": "meta"},
	})
	ID++
	events = append(events, &activity.Event{
		Timestamp:   time.Now().UTC(),
		Activity:    activity.SetupKeyOverused,
		ID:          ID,
		InitiatorID: userID,
		TargetID:    "setup-key-id",
		AccountID:   accountID,
		Meta:        map[string]any{"some": "meta"},
	})
	ID++
	events = append(events, &activity.Event{
		Timestamp:   time.Now().UTC(),
		Activity:    activity.SetupKeyCreated,
		ID:          ID,
		InitiatorID: userID,
		TargetID:    "setup-key-id",
		AccountID:   accountID,
		Meta:        map[string]any{"some": "meta"},
	})
	ID++
	events = append(events, &activity.Event{
		Timestamp:   time.Now().UTC(),
		Activity:    activity.RuleAdded,
		ID:          ID,
		InitiatorID: userID,
		TargetID:    "some-id",
		AccountID:   accountID,
		Meta:        map[string]any{"some": "meta"},
	})
	ID++
	events = append(events, &activity.Event{
		Timestamp:   time.Now().UTC(),
		Activity:    activity.RuleRemoved,
		ID:          ID,
		InitiatorID: userID,
		TargetID:    "some-id",
		AccountID:   accountID,
		Meta:        map[string]any{"some": "meta"},
	})
	ID++
	events = append(events, &activity.Event{
		Timestamp:   time.Now().UTC(),
		Activity:    activity.RuleUpdated,
		ID:          ID,
		InitiatorID: userID,
		TargetID:    "some-id",
		AccountID:   accountID,
		Meta:        map[string]any{"some": "meta"},
	})
	ID++
	events = append(events, &activity.Event{
		Timestamp:   time.Now().UTC(),
		Activity:    activity.PeerAddedWithSetupKey,
		ID:          ID,
		InitiatorID: userID,
		TargetID:    "some-id",
		AccountID:   accountID,
		Meta:        map[string]any{"some": "meta"},
	})
	return events
}

func TestEvents_GetEvents(t *testing.T) {
	tt := []struct {
		name           string
		expectedStatus int
		expectedBody   bool
		requestType    string
		requestPath    string
		requestBody    io.Reader
	}{
		{
			name:           "getAllEvents OK",
			expectedBody:   true,
			requestType:    http.MethodGet,
			requestPath:    "/api/events/",
			expectedStatus: http.StatusOK,
		},
	}
	accountID := "test_account"
	adminUser := types.NewAdminUser("test_user")
	events := generateEvents(accountID, adminUser.Id)
	handler := initEventsTestData(accountID, events...)

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(tc.requestType, tc.requestPath, tc.requestBody)
			req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{
				UserId:    "test_user",
				Domain:    "hotmail.com",
				AccountId: "test_account",
			})

			router := mux.NewRouter()
			router.HandleFunc("/api/events/", handler.getAllEvents).Methods("GET")
			router.ServeHTTP(recorder, req)

			res := recorder.Result()
			defer res.Body.Close()

			if status := recorder.Code; status != tc.expectedStatus {
				t.Errorf("handler returned wrong status code: got %v want %v",
					status, tc.expectedStatus)
				return
			}

			if !tc.expectedBody {
				return
			}

			content, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatalf("I don't know what I expected; %v", err)
			}

			var got []*api.Event
			if err = json.Unmarshal(content, &got); err != nil {
				t.Fatalf("Sent content is not in correct json format; %v", err)
			}

			assert.Len(t, got, len(events))
			actual := map[string]*api.Event{}
			for _, event := range got {
				actual[event.Id] = event
			}

			for _, expected := range events {
				event, ok := actual[strconv.FormatUint(expected.ID, 10)]
				assert.True(t, ok)
				assert.Equal(t, expected.InitiatorID, event.InitiatorId)
				assert.Equal(t, expected.TargetID, event.TargetId)
				assert.Equal(t, expected.Activity.Message(), event.Activity)
				assert.Equal(t, expected.Activity.StringCode(), string(event.ActivityCode))
				assert.Equal(t, expected.Meta["some"], event.Meta["some"])
				assert.True(t, expected.Timestamp.Equal(event.Timestamp))
			}
		})
	}
}
