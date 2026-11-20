// Essera fork: on a managed gateway the pages of the Console's people read the Console (its
// internal API, essera/consoleapi) in new-api's response formats, so the web UI stays new-api's:
// usage logs, the dashboard, audit, the account summary, the tenant's people, the service
// status and the Model Square. Operators keep new-api's own pages.
package controller

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/configsync"
	"github.com/QuantumNous/new-api/essera/consoleapi"
	"github.com/QuantumNous/new-api/essera/identity"
	"github.com/QuantumNous/new-api/essera/policy"
	"github.com/QuantumNous/new-api/essera/records"
	"github.com/QuantumNous/new-api/essera/settings"
	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// esseraPagePerson is the active person a managed gateway serves the page to from the Console.
// handled is false for an operator, or on an unmanaged gateway: their pages stay new-api's.
// When handled with no person, the request has been answered.
func esseraPagePerson(c *gin.Context) (person *snapshot.Person, handled bool) {
	if !settings.Managed() {
		return nil, false
	}
	userID := c.GetInt("id")
	isPerson, err := identity.IsPerson(userID)
	if err != nil {
		common.ApiError(c, err)
		return nil, true
	}
	if !isPerson {
		return nil, false
	}
	person, err = identity.ActivePerson(userID, snapshot.Current())
	if err != nil {
		common.ApiErrorStatus(c, http.StatusForbidden, common.NewMessage("This account cannot sign in to the Essera gateway."))
		return nil, true
	}
	return person, true
}

// writeConsoleError answers a failed Console call: a refusal with the sentence for its status,
// anything else as data that is temporarily unavailable.
func writeConsoleError(c *gin.Context, err error) {
	var refused *consoleapi.Refused
	if !errors.As(err, &refused) {
		common.ApiErrorStatus(c, http.StatusOK, common.NewMessage(consoleapi.ErrUnavailable.Error()))
		return
	}
	message := "The Console refused this request."
	switch refused.Status {
	case http.StatusForbidden:
		message = "Only an admin can do this."
	case http.StatusNotFound:
		message = "This person was not found."
	case http.StatusConflict:
		message = "A person with this email already exists."
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		message = "Check the email and name and try again."
	}
	common.ApiErrorStatus(c, refused.Status, common.NewMessage(message))
}

func consoleWindow(c *gin.Context) consoleapi.Window {
	var window consoleapi.Window
	if start, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64); start > 0 {
		window.From = time.Unix(start, 0)
	}
	if end, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64); end > 0 {
		window.To = time.Unix(end, 0)
	}
	return window
}

// quotaOf is a Console amount in new-api's quota units, so the UI's currency display shows it.
func quotaOf(amount string) int {
	value, err := decimal.NewFromString(amount)
	if err != nil {
		return 0
	}
	return int(value.Mul(decimal.NewFromFloat(common.QuotaPerUnit)).Round(0).IntPart())
}

// spendIn is the amount in the currency, or in the first currency by code when that one has
// none (a customer whose currency changed within the window).
func spendIn(spend map[string]string, currency string) string {
	if amount, ok := spend[currency]; ok {
		return amount
	}
	codes := slices.Sorted(maps.Keys(spend))
	if len(codes) == 0 {
		return "0"
	}
	return spend[codes[0]]
}

// customerCurrency is the currency the person's customer is billed in.
func customerCurrency(person *snapshot.Person) string {
	view := snapshot.Current()
	if view == nil {
		return ""
	}
	customer := view.Customer(person.CustomerID)
	if customer == nil {
		return ""
	}
	return view.Currency(customer.Entitlements)
}

// keyNames names the person's own keys by their key id, as the usage rows carry them.
func keyNames(userID int) map[string]string {
	var rows []struct {
		KeyID string
		Name  string
	}
	model.DB.Table(records.Key{}.TableName()+" AS k").Select("k.key_id, t.name").
		Joins("JOIN tokens AS t ON t.id = k.token_id").Where("k.user_id = ?", userID).Scan(&rows)
	names := make(map[string]string, len(rows))
	for _, row := range rows {
		names[row.KeyID] = row.Name
	}
	return names
}

// peopleByEndUser names the people of a customer by their end user's external id.
func peopleByEndUser(view *snapshot.View, customerID string) map[string]string {
	names := map[string]string{}
	if view == nil || view.Snapshot.Accounts == nil {
		return names
	}
	for _, person := range view.Snapshot.Accounts.People {
		if person.CustomerID == customerID && person.EndUserExternalID != nil {
			names[*person.EndUserExternalID] = person.Email
		}
	}
	return names
}

// esseraUserLogs serves /api/log/self from the Console's ledger.
func esseraUserLogs(c *gin.Context, person *snapshot.Person) {
	pageInfo := common.GetPageQuery(c)
	if logType, _ := strconv.Atoi(c.Query("type")); logType != model.LogTypeUnknown && logType != model.LogTypeConsume {
		pageInfo.SetItems([]model.Log{})
		common.ApiSuccess(c, pageInfo)
		return
	}
	filter := consoleapi.UsageFilter{Window: consoleWindow(c), Model: c.Query("model_name"), Page: pageInfo.Page, PageSize: pageInfo.PageSize}
	names := keyNames(c.GetInt("id"))
	if name := c.Query("token_name"); name != "" {
		for keyID, keyName := range names {
			if keyName == name {
				filter.KeyID = keyID
			}
		}
		if filter.KeyID == "" {
			pageInfo.SetItems([]model.Log{})
			common.ApiSuccess(c, pageInfo)
			return
		}
	}
	page, err := consoleapi.GetUsage(c.Request.Context(), person.ID, filter)
	if err != nil {
		writeConsoleError(c, err)
		return
	}
	people := peopleByEndUser(snapshot.Current(), person.CustomerID)
	logs := make([]model.Log, 0, len(page.Data))
	for i, row := range page.Data {
		logs = append(logs, consoleLog(pageInfo.GetStartIdx()+i+1, row, names, people, person.Email))
	}
	pageInfo.SetTotal(page.Total)
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
}

func consoleLog(id int, row consoleapi.UsageRow, keyNames, people map[string]string, ownEmail string) model.Log {
	log := model.Log{
		Id: id, CreatedAt: row.TS.Unix(), Type: model.LogTypeConsume, ModelName: row.Model,
		PromptTokens: int(row.InputUnits), CompletionTokens: int(row.OutputUnits), Username: ownEmail,
	}
	if row.Status != "succeeded" {
		log.Type = model.LogTypeError
	}
	if row.RequestID != nil {
		log.RequestId = *row.RequestID
	}
	if row.LatencyMS != nil {
		log.UseTime = int(*row.LatencyMS / 1000)
	}
	if row.KeyID != nil {
		log.TokenName = *row.KeyID
		if name, ok := keyNames[*row.KeyID]; ok {
			log.TokenName = name
		}
	}
	if row.EndUser != nil {
		log.Username = *row.EndUser
		if email, ok := people[*row.EndUser]; ok {
			log.Username = email
		}
	}
	details := map[string]any{"provider": row.Provider, "modality": row.Modality, "status": row.Status, "generated_units": row.GeneratedUnits}
	if row.BillableAmount != nil {
		details["billable_amount"] = *row.BillableAmount
		log.Quota = quotaOf(*row.BillableAmount)
	}
	if row.BillableCurrency != nil {
		details["currency"] = *row.BillableCurrency
	}
	if row.LatencyMS != nil {
		details["latency_ms"] = *row.LatencyMS
	}
	other, _ := common.Marshal(map[string]any{"essera": details})
	log.Other = string(other)
	return log
}

// esseraLogsStat serves /api/log/self/stat: what the window cost, the Console's total.
func esseraLogsStat(c *gin.Context, person *snapshot.Person) {
	series, err := consoleapi.GetUsageSeries(c.Request.Context(), person.ID, "day", consoleWindow(c))
	if err != nil {
		writeConsoleError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"quota": quotaOf(spendIn(series.Total.Spend, customerCurrency(person))), "rpm": 0, "tpm": 0})
}

// esseraQuotaDates serves /api/data/self, the dashboard's series: the Console's totals by hour
// or day and model, one point each, as they are.
func esseraQuotaDates(c *gin.Context, person *snapshot.Person) {
	bucket := "hour"
	if granularity := c.Query("default_time"); granularity == "day" || granularity == "week" {
		bucket = "day"
	}
	series, err := consoleapi.GetUsageSeries(c.Request.Context(), person.ID, bucket, consoleWindow(c))
	if err != nil {
		writeConsoleError(c, err)
		return
	}
	currency := customerCurrency(person)
	dates := make([]*model.QuotaData, 0, len(series.Rows))
	for _, row := range series.Rows {
		dates = append(dates, &model.QuotaData{
			ModelName: row.Model, CreatedAt: row.TS.Unix(),
			Count: int(row.Requests), TokenUsed: int(row.InputUnits + row.OutputUnits), Quota: quotaOf(spendIn(row.Spend, currency)),
		})
	}
	common.ApiSuccess(c, dates)
}

// esseraAuditLogs serves /api/audit/self from the Console's audit of the tenant (admin) or of
// the person and their keys (user).
func esseraAuditLogs(c *gin.Context, person *snapshot.Person) {
	page := common.GetPageQuery(c)
	if category := c.Query("category"); category != "" && category != model.AuditCategoryOperation {
		page.SetItems([]model.AuditLog{})
		common.ApiSuccess(c, page)
		return
	}
	rows, err := consoleapi.GetAudit(c.Request.Context(), person.ID, consoleWindow(c), page.Page, page.PageSize)
	if err != nil {
		writeConsoleError(c, err)
		return
	}
	view := snapshot.Current()
	logs := make([]model.AuditLog, 0, len(rows.Data))
	for i, row := range rows.Data {
		actor := "Essera"
		if row.Actor == "system" {
			actor = "system"
		}
		if row.ActorPersonID != nil {
			actor = *row.ActorPersonID
			if view != nil && view.Person(*row.ActorPersonID) != nil {
				actor = view.Person(*row.ActorPersonID).Email
			}
		}
		var resource []string
		for _, part := range []*string{row.ResourceType, row.ResourceID} {
			if part != nil && *part != "" {
				resource = append(resource, *part)
			}
		}
		// The web UI names the Console's actions by params.source; new_status tells a suspended
		// end user from a reactivated or removed one, which share end_user.update.
		params := model.AuditFields{"source": "console"}
		if row.NewStatus != nil {
			params["new_status"] = *row.NewStatus
		}
		logs = append(logs, model.AuditLog{
			Id: page.GetStartIdx() + i + 1, EventId: row.ID, Username: actor, CreatedAt: row.TS.Unix(),
			Category: model.AuditCategoryOperation, Action: row.Action, Success: row.Result == "success", Content: strings.Join(resource, " "),
			Other: model.AuditOther{Op: &model.AuditOperation{Action: row.Action, Params: params}},
		})
	}
	page.SetTotal(rows.Total)
	page.SetItems(logs)
	common.ApiSuccess(c, page)
}

// esseraAccount is the person's Console account for /api/user/self: this month's use against
// the limits, or that it is unavailable.
func esseraAccount(c *gin.Context, person *snapshot.Person) gin.H {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	me, err := consoleapi.GetMe(ctx, person.ID)
	if err != nil {
		return gin.H{"role": person.Role, "available": false}
	}
	currency := ""
	if me.Limits.Currency != nil {
		currency = *me.Limits.Currency
	}
	if currency == "" {
		currency = customerCurrency(person)
	}
	return gin.H{
		"role":          me.Person.Role,
		"available":     true,
		"customer_name": me.Customer.Name,
		"currency":      currency,
		"spend":         spendIn(me.Month.Spend, currency),
		"spend_limit":   me.Limits.MonthlySpendLimit,
		"spend_left":    me.Remaining.Spend,
		"requests":      me.Month.Requests,
		"request_quota": me.Limits.MonthlyRequestQuota,
		"tokens":        me.Month.InputUnits + me.Month.OutputUnits,
		"token_quota":   me.Limits.MonthlyTokenQuota,
	}
}

// esseraAdmin is the active admin who may manage their customer's people; anyone
// else is refused here, whatever the page showed them.
func esseraAdmin(c *gin.Context) (*snapshot.Person, bool) {
	person, handled := esseraPagePerson(c)
	if !handled {
		common.ApiErrorStatus(c, http.StatusForbidden, common.NewMessage("Only an admin can do this."))
		return nil, false
	}
	if person == nil {
		return nil, false
	}
	if person.Role != snapshot.RoleAdmin {
		common.ApiErrorStatus(c, http.StatusForbidden, common.NewMessage("Only an admin can do this."))
		return nil, false
	}
	return person, true
}

type esseraPersonItem struct {
	ID         string  `json:"id"`
	Email      string  `json:"email"`
	Name       *string `json:"name,omitempty"`
	Role       string  `json:"role"`
	Status     string  `json:"status"`
	ExternalID *string `json:"external_id,omitempty"`
	KeyCount   int     `json:"key_count"`
	Pending    bool    `json:"pending,omitempty"`
	Self       bool    `json:"self,omitempty"`
}

func personItem(person consoleapi.Person, admin *snapshot.Person) esseraPersonItem {
	item := esseraPersonItem{ID: person.ID, Name: person.Name, Role: person.Role, Status: person.Status, ExternalID: person.EndUserExternalID, KeyCount: person.Keys, Self: person.ID == admin.ID}
	if person.Email != nil {
		item.Email = *person.Email
	}
	return item
}

// EsseraPeople lists the tenant's people for its admin, from the Console.
func EsseraPeople(c *gin.Context) {
	admin, ok := esseraAdmin(c)
	if !ok {
		return
	}
	pageInfo := common.GetPageQuery(c)
	page, err := consoleapi.GetPeople(c.Request.Context(), admin.ID, pageInfo.Page, pageInfo.PageSize)
	if err != nil {
		writeConsoleError(c, err)
		return
	}
	items := make([]esseraPersonItem, 0, len(page.Data))
	for _, person := range page.Data {
		items = append(items, personItem(person, admin))
	}
	pageInfo.SetTotal(page.Total)
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

type addPersonRequest struct {
	Email      string `json:"email"`
	Name       string `json:"name"`
	ExternalID string `json:"external_id"`
}

// EsseraAddPerson asks the Console to add a user to the admin's customer. The person
// appears in the users table once the Console publishes them; until then the answer stands
// for them, marked pending.
func EsseraAddPerson(c *gin.Context) {
	admin, ok := esseraAdmin(c)
	if !ok {
		return
	}
	var request addPersonRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Check the email and name and try again."))
		return
	}
	email := model.NormalizeEmail(request.Email)
	name, externalID := strings.TrimSpace(request.Name), strings.TrimSpace(request.ExternalID)
	if !validEmail(email) || len(name) > 200 || len(externalID) > 200 {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Check the email and name and try again."))
		return
	}
	person := consoleapi.NewPerson{Email: email}
	if name != "" {
		person.Name = &name
	}
	if externalID != "" {
		person.ExternalID = &externalID
	}
	added, err := consoleapi.AddPerson(c.Request.Context(), admin.ID, person)
	if err != nil {
		writeConsoleError(c, err)
		return
	}
	item := personItem(*added, admin)
	item.Pending = true
	c.JSON(http.StatusCreated, gin.H{"success": true, "message": "", "data": item})
}

func validEmail(email string) bool {
	local, domain, found := strings.Cut(email, "@")
	return found && local != "" && strings.Contains(domain, ".") && len(email) <= 254 && !strings.ContainsAny(email, " \t\r\n<>,;")
}

// EsseraChangePerson suspends, reactivates or removes a user of the admin's customer, at the
// Console. Only users (role user) of the same customer, never an admin and never themselves;
// the Console checks the same. The users row follows at the next version.
func EsseraChangePerson(c *gin.Context) {
	admin, ok := esseraAdmin(c)
	if !ok {
		return
	}
	targetID := c.Param("id")
	view := snapshot.Current()
	var target *snapshot.Person
	if view != nil {
		target = view.Person(targetID)
	}
	if target == nil || target.CustomerID != admin.CustomerID {
		common.ApiErrorStatus(c, http.StatusNotFound, common.NewMessage("This person was not found."))
		return
	}
	if target.ID == admin.ID || target.Role != snapshot.RoleUser {
		common.ApiErrorStatus(c, http.StatusForbidden, common.NewMessage("Admins and your own account cannot be changed here."))
		return
	}
	ctx := c.Request.Context()
	var changed *consoleapi.Person
	var err error
	switch action := c.Param("action"); {
	case c.Request.Method == http.MethodDelete:
		err = consoleapi.RemovePerson(ctx, admin.ID, target.ID)
	case action == "suspend":
		changed, err = consoleapi.SuspendPerson(ctx, admin.ID, target.ID)
	case action == "reactivate":
		changed, err = consoleapi.ReactivatePerson(ctx, admin.ID, target.ID)
	default:
		common.ApiErrorStatus(c, http.StatusNotFound, common.NewMessage("Not found"))
		return
	}
	if err != nil {
		writeConsoleError(c, err)
		return
	}
	if changed == nil {
		common.ApiSuccess(c, gin.H{"id": target.ID, "removed": true, "pending": true})
		return
	}
	item := personItem(*changed, admin)
	item.Pending = true
	common.ApiSuccess(c, item)
}

// EsseraStatus shows the admin how the providers their customer may use are doing, and any
// maintenance announced, from the Console. While the Console does not answer, the status is
// said to be unavailable rather than guessed.
func EsseraStatus(c *gin.Context) {
	admin, ok := esseraAdmin(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
	defer cancel()
	status, err := consoleapi.GetStatus(ctx, admin.ID)
	var refused *consoleapi.Refused
	if errors.As(err, &refused) {
		writeConsoleError(c, err)
		return
	}
	if err != nil {
		common.ApiSuccess(c, gin.H{"available": false})
		return
	}
	providers := status.Providers
	if providers == nil {
		providers = []consoleapi.ProviderStatus{}
	}
	common.ApiSuccess(c, gin.H{"available": true, "providers": providers, "maintenance": status.Maintenance})
}

type esseraPricing struct {
	model.Pricing
	DisplayName  string             `json:"display_name"`
	ModelType    string             `json:"model_type"`
	EsseraPrices []consoleapi.Price `json:"essera_prices,omitempty"`
}

// esseraPricingList serves the Model Square of a managed gateway. A signed-in person sees only
// the models relay lets them call (policy.Available), with their prices as the Console rates
// them; a person who is not active sees none. A guest, or an operator, sees the active models
// of the active providers without prices. While the Console is down there are no prices.
func esseraPricingList(c *gin.Context) {
	view := snapshot.Current()
	var person *snapshot.Person
	var available map[string][]string
	if userID := c.GetInt("id"); userID > 0 && view != nil {
		isPerson, err := identity.IsPerson(userID)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if isPerson {
			available = map[string][]string{}
			if active, err := identity.ActivePerson(userID, view); err == nil {
				if caller, failure := identity.Caller(view, active, nil); failure == nil {
					person, available = active, policy.Available(caller)
				}
			}
		}
	}
	pricesAvailable := false
	byModel := map[string][]consoleapi.Price{}
	if person != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		prices, err := consoleapi.GetPrices(ctx, person.ID)
		cancel()
		if err == nil {
			pricesAvailable = true
			for _, price := range prices {
				key := price.Provider + "/" + price.Model
				byModel[key] = append(byModel[key], price)
			}
		}
	}
	vendors := []gin.H{}
	models := []esseraPricing{}
	if view != nil {
		for vendorID, provider := range view.Providers() {
			if provider.Status != snapshot.StatusActive {
				continue
			}
			listed := len(models)
			for _, providerModel := range provider.Models {
				if providerModel.Status != snapshot.StatusActive {
					continue
				}
				if available != nil && !slices.Contains(available[providerModel.Key], configsync.TagPrefix+provider.Key) {
					continue
				}
				models = append(models, esseraPricing{
					Pricing:      model.Pricing{ModelName: providerModel.Key, VendorID: vendorID + 1, OwnerBy: provider.Name, EnableGroup: []string{}},
					DisplayName:  providerModel.DisplayName,
					ModelType:    providerModel.Modality,
					EsseraPrices: byModel[provider.Key+"/"+providerModel.Key],
				})
			}
			if len(models) > listed {
				vendors = append(vendors, gin.H{"id": vendorID + 1, "name": provider.Name})
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success":            true,
		"data":               models,
		"vendors":            vendors,
		"group_ratio":        map[string]float64{},
		"usable_group":       map[string]string{},
		"supported_endpoint": model.GetSupportedEndpointMap(),
		"auto_groups":        []string{},
		"essera":             gin.H{"signed_in": person != nil, "prices_available": pricesAvailable},
	})
}
