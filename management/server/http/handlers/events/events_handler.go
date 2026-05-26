package events

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"

	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"

	"github.com/netbirdio/netbird/management/internals/modules/networktraffic"
	"github.com/netbirdio/netbird/management/server/account"
	"github.com/netbirdio/netbird/management/server/activity"
	nbcontext "github.com/netbirdio/netbird/management/server/context"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/shared/management/http/api"
	"github.com/netbirdio/netbird/shared/management/http/util"
)

// handler HTTP handler
type handler struct {
	accountManager account.Manager
}

func AddEndpoints(accountManager account.Manager, router *mux.Router) {
	eventsHandler := newHandler(accountManager)
	router.HandleFunc("/events", eventsHandler.getAllEvents).Methods("GET", "OPTIONS")
	router.HandleFunc("/events/audit", eventsHandler.getAllEvents).Methods("GET", "OPTIONS")
	router.HandleFunc("/events/network-traffic", eventsHandler.getAllNetworkTrafficEvents).Methods("GET", "OPTIONS")
	router.HandleFunc("/events/network-traffic/summary", eventsHandler.getNetworkTrafficSummary).Methods("GET", "OPTIONS")
}

// newHandler creates a new events handler
func newHandler(accountManager account.Manager) *handler {
	return &handler{accountManager: accountManager}
}

// getAllEvents list of the given account
func (h *handler) getAllEvents(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		log.WithContext(r.Context()).Error(err)
		http.Redirect(w, r, "/", http.StatusInternalServerError)
		return
	}

	accountID, userID := userAuth.AccountId, userAuth.UserId

	accountEvents, err := h.accountManager.GetEvents(r.Context(), accountID, userID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	events := make([]*api.Event, len(accountEvents))
	for i, e := range accountEvents {
		events[i] = toEventResponse(e)
	}

	util.WriteJSONObject(r.Context(), w, events)
}

func toEventResponse(event *activity.Event) *api.Event {
	meta := make(map[string]string)
	if event.Meta != nil {
		for s, a := range event.Meta {
			meta[s] = fmt.Sprintf("%v", a)
		}
	}
	e := &api.Event{
		Id:             fmt.Sprint(event.ID),
		InitiatorId:    event.InitiatorID,
		InitiatorName:  event.InitiatorName,
		InitiatorEmail: event.InitiatorEmail,
		Activity:       event.Activity.Message(),
		ActivityCode:   api.EventActivityCode(event.Activity.StringCode()),
		TargetId:       event.TargetID,
		Timestamp:      event.Timestamp,
		Meta:           meta,
	}
	return e
}

func (h *handler) getAllNetworkTrafficEvents(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	var filter networktraffic.Filter
	filter.ParseFromRequest(r)

	events, totalCount, err := h.accountManager.GetStore().GetAccountNetworkTrafficEvents(
		r.Context(),
		store.LockingStrengthNone,
		userAuth.AccountId,
		filter,
	)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	apiEvents := make([]api.NetworkTrafficEvent, 0, len(events))
	for _, event := range events {
		apiEvents = append(apiEvents, *event.ToAPIResponse())
	}
	if filter.AggregateFlows != nil && *filter.AggregateFlows {
		apiEvents = aggregateNetworkTrafficFlowEvents(apiEvents)
	}

	util.WriteJSONObject(r.Context(), w, &api.NetworkTrafficEventsResponse{
		Data:         apiEvents,
		Page:         filter.Page,
		PageSize:     filter.PageSize,
		TotalRecords: int(totalCount),
		TotalPages:   getTotalPageCount(int(totalCount), filter.PageSize),
	})
}

func aggregateNetworkTrafficFlowEvents(events []api.NetworkTrafficEvent) []api.NetworkTrafficEvent {
	flows := make(map[string]*api.NetworkTrafficEvent)
	order := make([]string, 0, len(events))

	for _, event := range events {
		flow, ok := flows[event.FlowId]
		if !ok {
			flowCopy := event
			flowCopy.Events = append([]api.NetworkTrafficSubEvent{}, event.Events...)
			flows[event.FlowId] = &flowCopy
			order = append(order, event.FlowId)
			continue
		}

		flow.Events = append(flow.Events, event.Events...)
		if event.TxBytes > flow.TxBytes {
			flow.TxBytes = event.TxBytes
		}
		if event.RxBytes > flow.RxBytes {
			flow.RxBytes = event.RxBytes
		}
		if event.TxPackets > flow.TxPackets {
			flow.TxPackets = event.TxPackets
		}
		if event.RxPackets > flow.RxPackets {
			flow.RxPackets = event.RxPackets
		}
	}

	result := make([]api.NetworkTrafficEvent, 0, len(order))
	for _, flowID := range order {
		flow := flows[flowID]
		sort.SliceStable(flow.Events, func(i, j int) bool {
			return flow.Events[i].Timestamp.After(flow.Events[j].Timestamp)
		})
		result = append(result, *flow)
	}
	return result
}

type networkTrafficSummaryPoint struct {
	Timestamp string `json:"timestamp"`
	RxBytes   int64  `json:"rx_bytes"`
	TxBytes   int64  `json:"tx_bytes"`
}

type networkTrafficSummaryResponse struct {
	Data []networkTrafficSummaryPoint `json:"data"`
}

func (h *handler) getNetworkTrafficSummary(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	var filter networktraffic.Filter
	filter.ParseFromRequest(r)

	bucketSeconds := parseBucketSeconds(r)
	points, err := h.accountManager.GetStore().GetAccountNetworkTrafficSummary(
		r.Context(),
		userAuth.AccountId,
		filter,
		bucketSeconds,
	)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	response := networkTrafficSummaryResponse{Data: make([]networkTrafficSummaryPoint, 0, len(points))}
	for _, point := range points {
		response.Data = append(response.Data, networkTrafficSummaryPoint{
			Timestamp: point.Timestamp.Format("2006-01-02T15:04:05.000Z07:00"),
			RxBytes:   point.RxBytes,
			TxBytes:   point.TxBytes,
		})
	}
	util.WriteJSONObject(r.Context(), w, response)
}

func parseBucketSeconds(r *http.Request) int {
	bucketSeconds, err := strconv.Atoi(r.URL.Query().Get("bucket_seconds"))
	if err != nil || bucketSeconds <= 0 {
		return 300
	}
	if bucketSeconds < 60 {
		return 60
	}
	if bucketSeconds > 24*60*60 {
		return 24 * 60 * 60
	}
	return bucketSeconds
}

func getTotalPageCount(totalCount, pageSize int) int {
	if pageSize <= 0 {
		return 0
	}
	return (totalCount + pageSize - 1) / pageSize
}
