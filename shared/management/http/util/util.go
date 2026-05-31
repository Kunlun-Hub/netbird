package util

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/netbirdio/netbird/shared/management/status"
)

// EmptyObject is an empty struct used to return empty JSON object
type EmptyObject struct {
}

type ErrorResponse struct {
	Message      string `json:"message"`
	Code         int    `json:"code"`
	ErrorCode    string `json:"error_code,omitempty"`
	Feature      string `json:"feature,omitempty"`
	Limit        string `json:"limit,omitempty"`
	Current      *int   `json:"current,omitempty"`
	Allowed      *int   `json:"allowed,omitempty"`
	RequiredPlan string `json:"required_plan,omitempty"`
}

// WriteJSONObject simply writes object to the HTTP response in JSON format
func WriteJSONObject(ctx context.Context, w http.ResponseWriter, obj interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(w).Encode(obj)
	if err != nil {
		WriteError(ctx, err, w)
		return
	}
}

// Duration is used strictly for JSON requests/responses due to duration marshalling issues
type Duration struct {
	time.Duration
}

// MarshalJSON marshals the duration
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

// UnmarshalJSON unmarshals the duration
func (d *Duration) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch value := v.(type) {
	case float64:
		d.Duration = time.Duration(value)
		return nil
	case string:
		var err error
		d.Duration, err = time.ParseDuration(value)
		if err != nil {
			return err
		}
		return nil
	default:
		return errors.New("invalid duration")
	}
}

// WriteErrorResponse prepares and writes an error response i nJSON
func WriteErrorResponse(errMsg string, httpStatus int, w http.ResponseWriter) {
	writeErrorResponse(&ErrorResponse{
		Message: errMsg,
		Code:    httpStatus,
	}, httpStatus, w)
}

func writeErrorResponse(response *ErrorResponse, httpStatus int, w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(httpStatus)
	err := json.NewEncoder(w).Encode(response)
	if err != nil {
		http.Error(w, "failed handling request", http.StatusInternalServerError)
	}
}

// WriteError converts an error to an JSON error response.
// If it is known internal error of type server.Error then it sets the messages from the error, a generic message otherwise
func WriteError(ctx context.Context, err error, w http.ResponseWriter) {
	log.WithContext(ctx).Errorf("got a handler error: %s", err.Error())
	errStatus, ok := status.FromError(err)
	httpStatus := http.StatusInternalServerError
	msg := "internal server error"
	if ok {
		switch errStatus.Type() {
		case status.UserAlreadyExists:
			httpStatus = http.StatusConflict
		case status.AlreadyExists:
			httpStatus = http.StatusConflict
		case status.PreconditionFailed:
			httpStatus = http.StatusPreconditionFailed
		case status.PermissionDenied:
			httpStatus = http.StatusForbidden
		case status.NotFound:
			httpStatus = http.StatusNotFound
		case status.Internal:
			httpStatus = http.StatusInternalServerError
		case status.InvalidArgument:
			httpStatus = http.StatusUnprocessableEntity
		case status.Unauthorized:
			httpStatus = http.StatusUnauthorized
		case status.BadRequest:
			httpStatus = http.StatusBadRequest
		case status.TooManyRequests:
			httpStatus = http.StatusTooManyRequests
		default:
		}
		msg = strings.ToLower(err.Error())
	} else {
		unhandledMSG := fmt.Sprintf("got unhandled error code, error: %s", err.Error())
		log.WithContext(ctx).Error(unhandledMSG)
	}

	response := &ErrorResponse{
		Message: msg,
		Code:    httpStatus,
	}
	if ok {
		applyStatusDetails(response, errStatus)
	}

	writeErrorResponse(response, httpStatus, w)
}

func applyStatusDetails(response *ErrorResponse, errStatus *status.Error) {
	if errStatus == nil {
		return
	}
	response.ErrorCode = errStatus.Code
	response.Feature = stringDetail(errStatus.Details, "feature")
	response.Limit = stringDetail(errStatus.Details, "limit")
	response.RequiredPlan = stringDetail(errStatus.Details, "required_plan")
	response.Current = intDetail(errStatus.Details, "current")
	response.Allowed = intDetail(errStatus.Details, "allowed")
}

func stringDetail(details map[string]interface{}, key string) string {
	value, ok := details[key]
	if !ok {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}

func intDetail(details map[string]interface{}, key string) *int {
	value, ok := details[key]
	if !ok {
		return nil
	}
	var result int
	switch v := value.(type) {
	case int:
		result = v
	case int8:
		result = int(v)
	case int16:
		result = int(v)
	case int32:
		result = int(v)
	case int64:
		result = int(v)
	case uint:
		result = int(v)
	case uint8:
		result = int(v)
	case uint16:
		result = int(v)
	case uint32:
		result = int(v)
	case uint64:
		result = int(v)
	case float64:
		result = int(v)
	default:
		return nil
	}
	return &result
}
