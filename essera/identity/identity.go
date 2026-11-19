// Package identity signs the Console's people in to the Gateway's web UI. The Gateway's own
// sign-in page talks to the customer pool through the Cognito user-pool API (signin.go); a
// person signs in only while active in the active configuration, as the users row configsync
// built for them, and their Gateway session lasts only as long as the Cognito sign-in behind it
// (session.go). Only operators (root) keep new-api's password sign-in.
package identity

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/auth"
	"github.com/QuantumNous/new-api/essera/records"
	"github.com/QuantumNous/new-api/essera/settings"
	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

// LoginMethod names a session started by the Essera sign-in.
const LoginMethod = "essera"

// Refusal is why someone may not act as a person; Reason is for the log only.
type Refusal struct{ Reason string }

func (*Refusal) Error() string { return "This account cannot sign in to the Essera gateway." }

func refuse(reason string) *Refusal { return &Refusal{Reason: reason} }

// LoginAllowed applies who may start a session on a managed gateway, whatever the sign-in
// path: root operators by the methods new-api offers them, a person only through the Essera
// sign-in and while active, and nobody else (the service user, leftover accounts).
func LoginAllowed(userID, role int, method string) (bool, error) {
	if !settings.Managed() || role >= common.RoleRootUser {
		return true, nil
	}
	if method != LoginMethod {
		return false, nil
	}
	_, err := ActivePerson(userID, snapshot.Current())
	var refusal *Refusal
	if errors.As(err, &refusal) {
		return false, nil
	}
	return err == nil, err
}

// IsPerson tells whether the user is a person's, active or not.
func IsPerson(userID int) (bool, error) {
	link, err := personLink("user_id = ?", userID)
	return link != nil, err
}

// ActivePerson is the person the user stands for, while active in view.
func ActivePerson(userID int, view *snapshot.View) (*snapshot.Person, error) {
	link, err := personLink("user_id = ?", userID)
	if err != nil {
		return nil, err
	}
	if link == nil {
		return nil, refuse("user is no person")
	}
	if view == nil {
		return nil, refuse("no configuration is active")
	}
	person := view.Person(link.PersonID)
	if person == nil || person.Status != snapshot.StatusActive {
		return nil, refuse("person " + link.PersonID + " is not active")
	}
	return person, nil
}

func personLink(condition string, value any) (*records.PersonUser, error) {
	var link records.PersonUser
	err := model.DB.Where(condition, value).First(&link).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &link, nil
}

// personUser is the enabled user of the active person with this Cognito sub.
func personUser(sub string) (*model.User, error) {
	view := snapshot.Current()
	if view == nil {
		return nil, refuse("no configuration is active")
	}
	person := view.PersonBySub(sub)
	if person == nil {
		return nil, refuse("sub " + sub + " is no person of the configuration")
	}
	if person.Status != snapshot.StatusActive {
		return nil, refuse("person " + person.ID + " is " + person.Status)
	}
	link, err := personLink("person_id = ?", person.ID)
	if err != nil {
		return nil, err
	}
	if link == nil {
		return nil, refuse("person " + person.ID + " has no user yet")
	}
	var user model.User
	if err := model.DB.Where("id = ?", link.UserID).First(&user).Error; err != nil {
		return nil, refuse("the user of person " + person.ID + " is missing")
	}
	if user.OidcId != sub || user.Status != common.UserStatusEnabled {
		return nil, refuse("the user of person " + person.ID + " is not enabled for this sub")
	}
	return &user, nil
}

// Caller makes a person the caller of a request: their customer and its partner, with the
// credential they used, or none in the playground. The credential must be the person's own and
// of the same customer, so nothing in a configuration can move a person to another tenant.
func Caller(view *snapshot.View, person *snapshot.Person, credential *snapshot.Credential) (*auth.Caller, *auth.Failure) {
	notActive := &auth.Failure{Code: "essera_unauthenticated", Message: "This credential is not active."}
	if person == nil || person.Status != snapshot.StatusActive {
		return nil, notActive
	}
	caller := &auth.Caller{Person: person, View: view}
	if credential != nil {
		if credential.PersonID != person.ID || credential.CustomerID != person.CustomerID {
			return nil, notActive
		}
		caller.ClientID, caller.Credential = credential.KeyID, credential
	}
	caller.Customer = view.Customer(person.CustomerID)
	if caller.Customer == nil {
		return nil, &auth.Failure{Code: "essera_unauthenticated", Message: "This credential has no customer."}
	}
	caller.Partner = view.Partner(caller.Customer.PartnerID)
	return caller, nil
}
