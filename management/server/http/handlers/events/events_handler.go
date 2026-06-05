package events

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

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

const (
	networkTrafficSummaryBucketSeconds = networktraffic.SummaryBucketSeconds
	networkTrafficSummaryMaxPoints     = networktraffic.MaxDateRangeDays * 24 * 60
)

func AddEndpoints(accountManager account.Manager, router *mux.Router) {
	eventsHandler := newHandler(accountManager)
	router.HandleFunc("/events", eventsHandler.getAllEvents).Methods("GET", "OPTIONS")
	router.HandleFunc("/events/audit", eventsHandler.getAllEvents).Methods("GET", "OPTIONS")
	router.HandleFunc("/events/dns", eventsHandler.getAllDNSEvents).Methods("GET", "OPTIONS")
	router.HandleFunc("/events/network-traffic", eventsHandler.getAllNetworkTrafficEvents).Methods("GET", "OPTIONS")
	router.HandleFunc("/events/network-traffic/groups", eventsHandler.getNetworkTrafficClientGroups).Methods("GET", "OPTIONS")
	router.HandleFunc("/events/network-traffic/group-flows", eventsHandler.getNetworkTrafficGroupFlows).Methods("GET", "OPTIONS")
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

func (h *handler) getAllDNSEvents(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	var filter networktraffic.Filter
	filter.ParseFromRequest(r)
	dnsOnly := true
	aggregateFlows := false
	filter.DNS = &dnsOnly
	filter.AggregateFlows = &aggregateFlows
	filter.InternalDNS = nil

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

	apiEvents := make([]dnsEventResponse, 0, len(events))
	for _, event := range events {
		apiEvents = append(apiEvents, toDNSEventResponse(event))
	}

	util.WriteJSONObject(r.Context(), w, &dnsEventsResponse{
		Data:         apiEvents,
		Page:         filter.Page,
		PageSize:     filter.PageSize,
		TotalRecords: int(totalCount),
		TotalPages:   getTotalPageCount(int(totalCount), filter.PageSize),
	})
}

type dnsEventResponse struct {
	ID          string                     `json:"id"`
	Timestamp   time.Time                  `json:"timestamp"`
	ReporterID  string                     `json:"reporter_id"`
	User        api.NetworkTrafficUser     `json:"user"`
	Device      api.NetworkTrafficEndpoint `json:"device"`
	Source      api.NetworkTrafficEndpoint `json:"source"`
	Destination api.NetworkTrafficEndpoint `json:"destination"`
	Domain      string                     `json:"domain"`
	QueryType   string                     `json:"query_type"`
	Answers     []string                   `json:"answers"`
	RCode       string                     `json:"rcode"`
}

type dnsEventsResponse struct {
	Data         []dnsEventResponse `json:"data"`
	Page         int                `json:"page"`
	PageSize     int                `json:"page_size"`
	TotalRecords int                `json:"total_records"`
	TotalPages   int                `json:"total_pages"`
}

func toDNSEventResponse(event *networktraffic.Event) dnsEventResponse {
	source := api.NetworkTrafficEndpoint{
		Id:       event.SourceID,
		Type:     event.SourceType,
		Name:     event.SourceName,
		Address:  event.SourceAddress,
		DnsLabel: optionalString(event.SourceDNSLabel),
		Os:       optionalString(event.SourceOS),
		GeoLocation: api.NetworkTrafficLocation{
			CountryCode: event.SourceCountryCode,
			CityName:    event.SourceCityName,
		},
	}
	destination := api.NetworkTrafficEndpoint{
		Id:       event.DestinationID,
		Type:     event.DestinationType,
		Name:     event.DestinationName,
		Address:  event.DestinationAddress,
		DnsLabel: optionalString(event.DestinationDNSLabel),
		Os:       optionalString(event.DestinationOS),
		GeoLocation: api.NetworkTrafficLocation{
			CountryCode: event.DestinationCountryCode,
			CityName:    event.DestinationCityName,
		},
	}

	return dnsEventResponse{
		ID:          event.ID,
		Timestamp:   event.Timestamp,
		ReporterID:  event.ReporterID,
		User:        api.NetworkTrafficUser{Id: event.UserID, Name: event.UserName, Email: event.UserEmail},
		Device:      source,
		Source:      source,
		Destination: destination,
		Domain:      event.DNSDomain,
		QueryType:   event.DNSQueryType,
		Answers:     event.DNSAnswers,
		RCode:       event.DNSRCode,
	}
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

type networkTrafficClientGroupResponse struct {
	Id              string                       `json:"id"`
	ClientKey       string                       `json:"client_key"`
	Client          api.NetworkTrafficEndpoint   `json:"client"`
	User            api.NetworkTrafficUser       `json:"user"`
	LatestTimestamp time.Time                    `json:"latest_timestamp"`
	FlowCount       int64                        `json:"flow_count"`
	RxBytes         int64                        `json:"rx_bytes"`
	RxPackets       int64                        `json:"rx_packets"`
	TxBytes         int64                        `json:"tx_bytes"`
	TxPackets       int64                        `json:"tx_packets"`
	Protocols       []int                        `json:"protocols"`
	Destinations    []api.NetworkTrafficEndpoint `json:"destinations"`
}

type networkTrafficClientGroupsResponse struct {
	Data         []networkTrafficClientGroupResponse `json:"data"`
	Page         int                                 `json:"page"`
	PageSize     int                                 `json:"page_size"`
	TotalRecords int                                 `json:"total_records"`
	TotalPages   int                                 `json:"total_pages"`
}

func (h *handler) getNetworkTrafficClientGroups(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	var filter networktraffic.Filter
	filter.ParseFromRequest(r)

	groups, totalCount, err := h.accountManager.GetStore().GetAccountNetworkTrafficClientGroups(
		r.Context(),
		store.LockingStrengthNone,
		userAuth.AccountId,
		filter,
	)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	responseGroups := make([]networkTrafficClientGroupResponse, 0, len(groups))
	for _, group := range groups {
		destinations := make([]api.NetworkTrafficEndpoint, 0, len(group.Destinations))
		for _, destination := range group.Destinations {
			destinations = append(destinations, toNetworkTrafficEndpoint(destination))
		}
		responseGroups = append(responseGroups, networkTrafficClientGroupResponse{
			Id:              group.ID,
			ClientKey:       group.ID,
			Client:          toNetworkTrafficEndpoint(group.Client),
			User:            api.NetworkTrafficUser{Id: group.UserID, Name: group.UserName, Email: group.UserEmail},
			LatestTimestamp: group.LatestTimestamp,
			FlowCount:       group.FlowCount,
			RxBytes:         group.RxBytes,
			RxPackets:       group.RxPackets,
			TxBytes:         group.TxBytes,
			TxPackets:       group.TxPackets,
			Protocols:       group.Protocols,
			Destinations:    destinations,
		})
	}

	util.WriteJSONObject(r.Context(), w, &networkTrafficClientGroupsResponse{
		Data:         responseGroups,
		Page:         filter.Page,
		PageSize:     filter.PageSize,
		TotalRecords: int(totalCount),
		TotalPages:   getTotalPageCount(int(totalCount), filter.PageSize),
	})
}

func (h *handler) getNetworkTrafficGroupFlows(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	var filter networktraffic.Filter
	filter.ParseFromRequest(r)
	if filter.PageSize > 10000 {
		filter.PageSize = 10000
	}

	events, totalCount, err := h.accountManager.GetStore().GetAccountNetworkTrafficGroupFlows(
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
	apiEvents = aggregateNetworkTrafficFlowEvents(apiEvents)

	util.WriteJSONObject(r.Context(), w, &api.NetworkTrafficEventsResponse{
		Data:         apiEvents,
		Page:         filter.Page,
		PageSize:     filter.PageSize,
		TotalRecords: int(totalCount),
		TotalPages:   getTotalPageCount(int(totalCount), filter.PageSize),
	})
}

func toNetworkTrafficEndpoint(endpoint networktraffic.Endpoint) api.NetworkTrafficEndpoint {
	return api.NetworkTrafficEndpoint{
		Id:       endpoint.ID,
		Type:     endpoint.Type,
		Name:     endpoint.Name,
		Address:  endpoint.Address,
		DnsLabel: optionalString(endpoint.DNSLabel),
		Os:       optionalString(endpoint.OS),
		GeoLocation: api.NetworkTrafficLocation{
			CountryCode: endpoint.CountryCode,
			CityName:    endpoint.CityName,
		},
	}
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
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
	Timestamp      string  `json:"timestamp"`
	BucketStart    string  `json:"bucket_start"`
	BucketEnd      string  `json:"bucket_end"`
	CoveredSeconds float64 `json:"covered_seconds"`
	RxBytes        int64   `json:"rx_bytes"`
	TxBytes        int64   `json:"tx_bytes"`
	DownloadRate   float64 `json:"download_rate"`
	UploadRate     float64 `json:"upload_rate"`
}

type networkTrafficSummaryResponse struct {
	BucketSeconds int                          `json:"bucket_seconds"`
	Data          []networkTrafficSummaryPoint `json:"data"`
	DownloadPeak  float64                      `json:"download_peak"`
	DownloadTotal int64                        `json:"download_total"`
	UploadPeak    float64                      `json:"upload_peak"`
	UploadTotal   int64                        `json:"upload_total"`
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
	if countSummaryPoints(filter, bucketSeconds) > networkTrafficSummaryMaxPoints {
		util.WriteErrorResponse("network traffic summary range is too large", http.StatusBadRequest, w)
		return
	}

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

	response := networkTrafficSummaryResponse{
		BucketSeconds: bucketSeconds,
		Data:          make([]networkTrafficSummaryPoint, 0, len(points)),
	}
	for _, point := range points {
		response.DownloadTotal += point.RxBytes
		response.UploadTotal += point.TxBytes
		if point.DownloadRate > response.DownloadPeak {
			response.DownloadPeak = point.DownloadRate
		}
		if point.UploadRate > response.UploadPeak {
			response.UploadPeak = point.UploadRate
		}

		response.Data = append(response.Data, networkTrafficSummaryPoint{
			Timestamp:      point.Timestamp.Format("2006-01-02T15:04:05.000Z07:00"),
			BucketStart:    point.BucketStart.Format("2006-01-02T15:04:05.000Z07:00"),
			BucketEnd:      point.BucketEnd.Format("2006-01-02T15:04:05.000Z07:00"),
			CoveredSeconds: point.CoveredSeconds,
			RxBytes:        point.RxBytes,
			TxBytes:        point.TxBytes,
			DownloadRate:   point.DownloadRate,
			UploadRate:     point.UploadRate,
		})
	}
	util.WriteJSONObject(r.Context(), w, response)
}

func parseBucketSeconds(r *http.Request) int {
	bucketSeconds, err := strconv.Atoi(r.URL.Query().Get("bucket_seconds"))
	if err != nil || bucketSeconds <= 0 {
		return networkTrafficSummaryBucketSeconds
	}
	if bucketSeconds < networkTrafficSummaryBucketSeconds {
		return networkTrafficSummaryBucketSeconds
	}
	if bucketSeconds > 24*60*60 {
		return 24 * 60 * 60
	}
	return bucketSeconds
}

func countSummaryPoints(filter networktraffic.Filter, bucketSeconds int) int64 {
	startTime, endTime := networkTrafficSummaryRange(filter)
	if !endTime.After(startTime) {
		return 0
	}

	firstBucket := startTime.Unix() / int64(bucketSeconds)
	lastBucket := endTime.Add(-time.Nanosecond).Unix() / int64(bucketSeconds)
	return lastBucket - firstBucket + 1
}

func networkTrafficSummaryRange(filter networktraffic.Filter) (time.Time, time.Time) {
	endTime := time.Now().UTC()
	if filter.EndDate != nil {
		endTime = filter.EndDate.UTC()
	}

	startTime := endTime.Add(-6 * time.Hour)
	if filter.StartDate != nil {
		startTime = filter.StartDate.UTC()
	}

	return startTime, endTime
}

func getTotalPageCount(totalCount, pageSize int) int {
	if pageSize <= 0 {
		return 0
	}
	return (totalCount + pageSize - 1) / pageSize
}
