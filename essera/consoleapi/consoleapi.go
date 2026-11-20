// Package consoleapi reads the Console's data for a signed-in person, and asks the Console to
// change the people of a tenant, through the Console's internal API (/internal/v1 at
// CONSOLE_INTERNAL_URL). Each call proves the Gateway's task role (servicetrust.Identity, for
// the audience essera-console-<env>) and names the person in X-Essera-Person; the Console
// scopes every answer to that person. Nothing the relay needs depends on it.
package consoleapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/servicetrust"
	"github.com/QuantumNous/new-api/essera/settings"
)

// PersonHeader names the signed-in person, whom the Gateway has verified.
const PersonHeader = "X-Essera-Person"

// ErrUnavailable means the Console did not answer usably: the pages say the data is
// temporarily unavailable.
var ErrUnavailable = errors.New("Data is temporarily unavailable")

// Refused is the Console's answer to a call it would not serve: 403, 404, 409 or 422.
type Refused struct {
	Status  int
	Message string
}

func (r *Refused) Error() string {
	return fmt.Sprintf("the Console refused (%d): %s", r.Status, r.Message)
}

var client = &http.Client{
	Timeout:       5 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// Identity proves the Gateway to the Console; replaced in tests.
var Identity = servicetrust.Identity

func baseURL() string {
	return strings.TrimRight(strings.TrimSpace(common.GetEnvOrDefaultString("CONSOLE_INTERNAL_URL", "")), "/")
}

// Audience is the Console in this environment, as its signature must name it.
func Audience() string {
	return "essera-console-" + settings.Environment()
}

func call(ctx context.Context, method, personID, path string, query url.Values, input, output any) error {
	base := baseURL()
	if base == "" || personID == "" {
		return ErrUnavailable
	}
	target := base + "/internal/v1" + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var body io.Reader
	if input != nil {
		encoded, err := common.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return err
	}
	identity, err := Identity(ctx, Audience())
	if err != nil {
		common.SysError(common.LogText("essera: signing a Console call: %s", err.Error()))
		return ErrUnavailable
	}
	request.Header.Set(servicetrust.Header, identity)
	request.Header.Set(PersonHeader, personID)
	request.Header.Set("Accept", "application/json")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		common.SysError(common.LogText("essera: Console %s %s: %s", method, path, err.Error()))
		return ErrUnavailable
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return ErrUnavailable
	}
	switch {
	case response.StatusCode >= 200 && response.StatusCode < 300:
		if output == nil || len(data) == 0 {
			return nil
		}
		if err := common.Unmarshal(data, output); err != nil {
			common.SysError(common.LogText("essera: Console %s %s answered unreadably: %s", method, path, err.Error()))
			return ErrUnavailable
		}
		return nil
	case response.StatusCode == http.StatusForbidden, response.StatusCode == http.StatusNotFound,
		response.StatusCode == http.StatusConflict, response.StatusCode == http.StatusUnprocessableEntity,
		response.StatusCode == http.StatusBadRequest:
		return &Refused{Status: response.StatusCode, Message: errorMessage(data)}
	}
	common.SysError(common.LogText("essera: Console %s %s answered %d", method, path, response.StatusCode))
	return ErrUnavailable
}

// errorMessage reads the Console's error envelope ({"error": {"message"}}) or FastAPI's
// ({"detail"}), for the log; people are told a fixed sentence per status.
func errorMessage(data []byte) string {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Detail any `json:"detail"`
	}
	if common.Unmarshal(data, &envelope) != nil {
		return ""
	}
	if envelope.Error.Message != "" {
		return envelope.Error.Message
	}
	if text, ok := envelope.Detail.(string); ok {
		return text
	}
	return ""
}

// Page is the Console's paging envelope.
type Page[T any] struct {
	Data     []T `json:"data"`
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}

type Person struct {
	ID                string  `json:"id"`
	Email             *string `json:"email"`
	Name              *string `json:"name"`
	Role              string  `json:"role"`
	Status            string  `json:"status"`
	EndUserExternalID *string `json:"end_user_external_id"`
	Keys              int     `json:"keys"`
}

type Customer struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type Month struct {
	Since          time.Time         `json:"since"`
	Requests       int64             `json:"requests"`
	InputUnits     int64             `json:"input_units"`
	OutputUnits    int64             `json:"output_units"`
	GeneratedUnits int64             `json:"generated_units"`
	Spend          map[string]string `json:"spend"`
}

type Limits struct {
	MonthlyRequestQuota *int64  `json:"monthly_request_quota"`
	MonthlyTokenQuota   *int64  `json:"monthly_token_quota"`
	MonthlySpendLimit   *string `json:"monthly_spend_limit"`
	Currency            *string `json:"currency"`
}

type Remaining struct {
	Requests *int64  `json:"requests"`
	Tokens   *int64  `json:"tokens"`
	Spend    *string `json:"spend"`
}

// Me is the person, their customer, this month's use and what is left of it.
type Me struct {
	Person    Person    `json:"person"`
	Customer  Customer  `json:"customer"`
	Month     Month     `json:"month"`
	Limits    Limits    `json:"limits"`
	Remaining Remaining `json:"remaining"`
}

type UsageRow struct {
	TS               time.Time `json:"ts"`
	RequestID        *string   `json:"request_id"`
	Provider         string    `json:"provider"`
	Model            string    `json:"model"`
	Modality         string    `json:"modality"`
	InputUnits       int64     `json:"input_units"`
	OutputUnits      int64     `json:"output_units"`
	GeneratedUnits   int64     `json:"generated_units"`
	BillableAmount   *string   `json:"billable_amount"`
	BillableCurrency *string   `json:"billable_currency"`
	Status           string    `json:"status"`
	KeyID            *string   `json:"key_id"`
	EndUser          *string   `json:"end_user"`
	LatencyMS        *int64    `json:"latency_ms"`
}

type AuditRow struct {
	ID            string    `json:"id"`
	TS            time.Time `json:"ts"`
	Action        string    `json:"action"`
	Actor         string    `json:"actor"`
	ActorPersonID *string   `json:"actor_person_id"`
	ResourceType  *string   `json:"resource_type"`
	ResourceID    *string   `json:"resource_id"`
	Result        string    `json:"result"`
	NewStatus     *string   `json:"new_status"`
}

// SeriesRow is the totals of one provider and model in one hour or day, ts its UTC start.
type SeriesRow struct {
	TS             time.Time         `json:"ts"`
	Provider       string            `json:"provider"`
	Model          string            `json:"model"`
	Requests       int64             `json:"requests"`
	InputUnits     int64             `json:"input_units"`
	OutputUnits    int64             `json:"output_units"`
	GeneratedUnits int64             `json:"generated_units"`
	Spend          map[string]string `json:"spend"`
}

type SeriesTotal struct {
	Requests       int64             `json:"requests"`
	InputUnits     int64             `json:"input_units"`
	OutputUnits    int64             `json:"output_units"`
	GeneratedUnits int64             `json:"generated_units"`
	Spend          map[string]string `json:"spend"`
}

// Series is the person's usage by hour or day and model, oldest first, with the window's
// totals; Truncated past the Console's row limit.
type Series struct {
	Bucket    string      `json:"bucket"`
	Rows      []SeriesRow `json:"rows"`
	Total     SeriesTotal `json:"total"`
	Truncated bool        `json:"truncated"`
}

// Window bounds a read in time; zero ends are open.
type Window struct {
	From, To time.Time
}

func (w Window) values(query url.Values) url.Values {
	if !w.From.IsZero() {
		query.Set("from", w.From.UTC().Format(time.RFC3339))
	}
	if !w.To.IsZero() {
		query.Set("to", w.To.UTC().Format(time.RFC3339))
	}
	return query
}

func paging(page, pageSize int) url.Values {
	query := url.Values{}
	query.Set("page", fmt.Sprint(max(page, 1)))
	query.Set("page_size", fmt.Sprint(min(max(pageSize, 1), 200)))
	return query
}

func GetMe(ctx context.Context, personID string) (*Me, error) {
	var me Me
	return &me, call(ctx, http.MethodGet, personID, "/me", nil, nil, &me)
}

// UsageFilter narrows the usage rows; empty fields do not.
type UsageFilter struct {
	Window
	Model, KeyID   string
	Page, PageSize int
}

func GetUsage(ctx context.Context, personID string, filter UsageFilter) (*Page[UsageRow], error) {
	query := filter.values(paging(filter.Page, filter.PageSize))
	if filter.Model != "" {
		query.Set("model", filter.Model)
	}
	if filter.KeyID != "" {
		query.Set("key_id", filter.KeyID)
	}
	var page Page[UsageRow]
	return &page, call(ctx, http.MethodGet, personID, "/usage", query, nil, &page)
}

// GetUsageSeries totals the usage by "hour" or "day" and model.
func GetUsageSeries(ctx context.Context, personID, bucket string, window Window) (*Series, error) {
	var series Series
	return &series, call(ctx, http.MethodGet, personID, "/usage/series", window.values(url.Values{"bucket": {bucket}}), nil, &series)
}

func GetAudit(ctx context.Context, personID string, window Window, page, pageSize int) (*Page[AuditRow], error) {
	var rows Page[AuditRow]
	return &rows, call(ctx, http.MethodGet, personID, "/audit", window.values(paging(page, pageSize)), nil, &rows)
}

func GetPeople(ctx context.Context, personID string, page, pageSize int) (*Page[Person], error) {
	var people Page[Person]
	return &people, call(ctx, http.MethodGet, personID, "/people", paging(page, pageSize), nil, &people)
}

// NewPerson is a user an admin adds; the Console creates them and their sign-in.
type NewPerson struct {
	Email      string  `json:"email"`
	Name       *string `json:"name,omitempty"`
	ExternalID *string `json:"external_id,omitempty"`
}

func AddPerson(ctx context.Context, adminID string, person NewPerson) (*Person, error) {
	var added Person
	return &added, call(ctx, http.MethodPost, adminID, "/people", nil, person, &added)
}

func SuspendPerson(ctx context.Context, adminID, personID string) (*Person, error) {
	var changed Person
	return &changed, call(ctx, http.MethodPost, adminID, "/people/"+url.PathEscape(personID)+"/suspend", nil, nil, &changed)
}

func ReactivatePerson(ctx context.Context, adminID, personID string) (*Person, error) {
	var changed Person
	return &changed, call(ctx, http.MethodPost, adminID, "/people/"+url.PathEscape(personID)+"/reactivate", nil, nil, &changed)
}

func RemovePerson(ctx context.Context, adminID, personID string) error {
	return call(ctx, http.MethodDelete, adminID, "/people/"+url.PathEscape(personID), nil, nil, nil)
}

// Price is the person's selling price of one unit of a model, as the Console rates it: Amount
// for PerQuantity units (1000000 tokens, 1 image, 1 second, 1 minute, 1 request).
type Price struct {
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	Unit        string `json:"unit"`
	Amount      string `json:"amount"`
	PerQuantity int64  `json:"per_quantity"`
	Currency    string `json:"currency"`
}

// GetPrices lists the person's prices of the models they may use.
func GetPrices(ctx context.Context, personID string) ([]Price, error) {
	var prices struct {
		Data []Price `json:"data"`
	}
	return prices.Data, call(ctx, http.MethodGet, personID, "/prices", nil, nil, &prices)
}

// ProviderStatus is how one provider the customer may use is doing: Status is "operational",
// "degraded", "down" or "maintenance"; Models counts the models the person may use there.
type ProviderStatus struct {
	Name                 string     `json:"name"`
	Region               string     `json:"region"`
	Status               string     `json:"status"`
	AvailabilityLastHour *float64   `json:"availability_last_hour"`
	P50MS                *int64     `json:"p50_ms"`
	CheckedAt            *time.Time `json:"checked_at"`
	Models               int        `json:"models"`
}

// Maintenance is Gateway maintenance the Console announced: Status "scheduled" or "in_progress".
type Maintenance struct {
	Status   string     `json:"status"`
	Message  string     `json:"message"`
	StartsAt *time.Time `json:"starts_at"`
	EndsAt   *time.Time `json:"ends_at"`
}

// Status is the state of the customer's providers and any announced maintenance.
type Status struct {
	Providers   []ProviderStatus `json:"providers"`
	Maintenance *Maintenance     `json:"maintenance"`
}

func GetStatus(ctx context.Context, personID string) (*Status, error) {
	var status Status
	return &status, call(ctx, http.MethodGet, personID, "/status", nil, nil, &status)
}
