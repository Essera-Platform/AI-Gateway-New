// Package policy decides whether an authenticated customer request may be served, in the
// order the Console's configuration asks: maintenance, account status, country, then monthly
// limits; and which models it may use on which providers.
package policy

import (
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/essera/auth"
	"github.com/QuantumNous/new-api/essera/configsync"
	"github.com/QuantumNous/new-api/essera/limits"
	"github.com/QuantumNous/new-api/essera/snapshot"
)

// Denial is why a request is refused before it reaches a provider.
type Denial struct {
	Status  int
	Code    string
	Reason  string // the telemetry bucket: maintenance, account_status, country, quota, model
	Message string
	Headers map[string]string
}

// Request is what the policy settled for a request that may be served.
type Request struct {
	Caller    *auth.Caller
	EndUserID string            // the customer's id for the end user, "" when not named
	EndUser   *snapshot.EndUser // nil when not named or not known
	Effective snapshot.Entitlements
	Currency  string
	Subjects  []limits.Subject
	Month     string
	// AllowedModels maps each model the request may use to the tags of the channels that may
	// serve it.
	AllowedModels map[string][]string
	Headers       map[string]string
	Reserved      bool      // counted against the request quotas, once it went to relay
	ReservedAt    time.Time // when, so an unserved request gives back that count
}

// Evaluate applies the policy to an authenticated caller.
func Evaluate(caller *auth.Caller, endUserID, country string, now time.Time) (*Request, *Denial) {
	view := caller.View
	request := &Request{Caller: caller, EndUserID: endUserID, Headers: map[string]string{}}
	if denial := maintenance(view.Snapshot.Gateway, now, request); denial != nil {
		return nil, denial
	}
	if denial := statuses(caller, endUserID, request); denial != nil {
		return nil, denial
	}
	effective := effectiveEntitlements(caller, request.EndUser)
	request.Effective = effective
	if allowed, code := view.CountryAllowed(effective, country); !allowed {
		return nil, &Denial{Status: http.StatusForbidden, Code: code, Reason: "country", Message: "Requests from this country are not served for this account."}
	}
	request.Currency = view.Currency(effective)
	request.Month = limits.Month(view, now)
	request.Subjects = subjects(caller, request.EndUser)
	exceeded, err := limits.Exceeded(request.Subjects, request.Month, request.Currency)
	if err != nil {
		return nil, &Denial{Status: http.StatusServiceUnavailable, Code: "essera_unavailable", Reason: "unavailable", Message: "The gateway cannot check this account's limits right now."}
	}
	if exceeded != "" {
		return nil, &Denial{Status: http.StatusTooManyRequests, Code: "essera_quota_exceeded", Reason: "quota", Message: "This account has reached its monthly " + exceeded + " limit."}
	}
	request.AllowedModels = allowedModels(view, effective)
	return request, nil
}

func maintenance(section snapshot.GatewaySection, now time.Time, request *Request) *Denial {
	switch section.MaintenanceStatus {
	case snapshot.MaintenanceActive:
		denial := &Denial{Status: http.StatusServiceUnavailable, Code: "essera_maintenance", Reason: "maintenance", Message: "The gateway is in maintenance."}
		if section.Message != "" {
			denial.Message = section.Message
		}
		if section.WindowEnd != nil && section.WindowEnd.After(now) {
			denial.Headers = map[string]string{"Retry-After": retryAfter(*section.WindowEnd, now)}
		}
		return denial
	case snapshot.MaintenanceScheduled:
		if section.WindowStart != nil && section.WindowEnd != nil && !now.Before(*section.WindowStart) && now.Before(*section.WindowEnd) {
			// The announced window has begun: it is maintenance even if nobody switched it on.
			active := section
			active.MaintenanceStatus = snapshot.MaintenanceActive
			return maintenance(active, now, request)
		}
		if section.WindowStart != nil && section.WindowEnd != nil {
			request.Headers["X-Essera-Maintenance"] = "scheduled; start=" + section.WindowStart.UTC().Format(time.RFC3339) + "; end=" + section.WindowEnd.UTC().Format(time.RFC3339)
		}
	}
	return nil
}

func retryAfter(end, now time.Time) string {
	return strconv.Itoa(max(int(end.Sub(now).Seconds())+1, 1))
}

func statuses(caller *auth.Caller, endUserID string, request *Request) *Denial {
	inactive := &Denial{Status: http.StatusForbidden, Code: "essera_account_inactive", Reason: "account_status", Message: "This account is not active."}
	if caller.Customer.Status != snapshot.StatusActive {
		return inactive
	}
	if caller.Partner != nil && caller.Partner.Status != snapshot.StatusActive {
		return inactive
	}
	if endUserID == "" {
		return nil
	}
	request.EndUser = caller.View.EndUser(caller.Customer.ID, endUserID)
	if request.EndUser != nil && request.EndUser.Status != snapshot.StatusActive {
		inactive.Message = "This end user is not active."
		return inactive
	}
	return nil
}

func subjects(caller *auth.Caller, endUser *snapshot.EndUser) []limits.Subject {
	var all []limits.Subject
	if caller.Partner != nil {
		all = append(all, limits.Subject{Scope: "partner", ID: caller.Partner.ID, Limits: caller.Partner.Entitlements})
	}
	all = append(all, limits.Subject{Scope: "customer", ID: caller.Customer.ID, Limits: caller.Customer.Entitlements})
	if caller.Credential != nil {
		all = append(all, limits.Subject{Scope: "credential", ID: caller.CredentialRef, Limits: caller.Credential.Limits})
	}
	if endUser != nil {
		all = append(all, limits.Subject{Scope: "end_user", ID: caller.Customer.ID + "/" + endUser.ExternalID, Limits: endUser.Limits})
	}
	return all
}

// allowedModels lists the servable models the entitlements allow, each with the channels of
// the providers that may serve it. A provider or model that is not active is never served,
// whatever an older account version says.
// effectiveEntitlements are the customer's entitlements narrowed by the credential's limits and
// the end user's.
func effectiveEntitlements(caller *auth.Caller, endUser *snapshot.EndUser) snapshot.Entitlements {
	effective := caller.Customer.Entitlements
	if caller.Credential != nil {
		effective = snapshot.Narrow(effective, caller.Credential.Limits)
	}
	if endUser != nil {
		effective = snapshot.Narrow(effective, endUser.Limits)
	}
	return effective
}

// Available are the models the caller may call, each with the tags of the channels that may
// serve it, by the same rules Evaluate applies to a request: none while the customer, its
// partner or the caller's end user is not active. A person's end user is their own.
func Available(caller *auth.Caller) map[string][]string {
	endUserID, personal := caller.EndUserID()
	if !personal {
		endUserID = ""
	}
	request := &Request{}
	if statuses(caller, endUserID, request) != nil {
		return map[string][]string{}
	}
	return allowedModels(caller.View, effectiveEntitlements(caller, request.EndUser))
}

func allowedModels(view *snapshot.View, effective snapshot.Entitlements) map[string][]string {
	allowed := map[string][]string{}
	for _, provider := range view.Providers() {
		if effective.Providers != nil && !slices.Contains(effective.Providers, provider.Key) {
			continue
		}
		for _, providerModel := range provider.Models {
			if !view.ModelServable(provider.Key, providerModel.Key) {
				continue
			}
			if effective.Models != nil && !slices.Contains(effective.Models, provider.Key+"/"+providerModel.Key) {
				continue
			}
			allowed[providerModel.Key] = append(allowed[providerModel.Key], configsync.TagPrefix+provider.Key)
		}
	}
	return allowed
}
