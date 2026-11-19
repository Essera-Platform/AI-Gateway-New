package configsync

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/records"
	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"gorm.io/gorm"
)

// personQuota keeps a person's new-api quota positive. Their group's ratio is zero, so it
// never runs down: limits are the policy's and prices the Console's.
const personQuota = 2_000_000_000

// projectPeople builds one users row per person of the snapshot, linked by oidc_id = sub,
// and disables the rows of people who are gone or not active, ending their sessions. Only
// rows linked in essera_people are ever changed, so operator users stay untouched. A person
// without a Cognito sub yet has no row until it is published. A person that cannot be
// projected, such as one whose email an operator account holds, is logged and skipped so it
// does not hold back the rest; failing to disable someone fails the version, which is retried.
func projectPeople(accounts *snapshot.AccountsCatalog) error {
	if accounts == nil || accounts.People == nil {
		return nil // a version from before the Console published people changes nobody
	}
	var links []records.PersonUser
	if err := model.DB.Find(&links).Error; err != nil {
		return err
	}
	linked := make(map[string]records.PersonUser, len(links))
	for _, link := range links {
		linked[link.PersonID] = link
	}
	published := map[string]bool{}
	for i := range accounts.People {
		person := &accounts.People[i]
		if person.ID == "" || person.Sub == nil || *person.Sub == "" {
			continue
		}
		published[person.ID] = true
		link, found := linked[person.ID]
		if !found {
			if err := createPersonUser(person); err != nil {
				common.SysError(common.LogText("essera: no user for person %s: %s", person.ID, err.Error()))
			}
			continue
		}
		if err := updatePersonUser(link, person); err != nil {
			if person.Status != snapshot.StatusActive {
				return fmt.Errorf("disabling the user of person %s: %w", person.ID, err)
			}
			common.SysError(common.LogText("essera: updating the user of person %s: %s", person.ID, err.Error()))
		}
	}
	for _, link := range links {
		if published[link.PersonID] {
			continue
		}
		if err := disableUser(link.UserID); err != nil {
			return fmt.Errorf("disabling the user of removed person %s: %w", link.PersonID, err)
		}
	}
	return nil
}

func userStatus(person *snapshot.Person) int {
	if person.Status == snapshot.StatusActive {
		return common.UserStatusEnabled
	}
	return common.UserStatusDisabled
}

func createPersonUser(person *snapshot.Person) error {
	email := model.NormalizeEmail(person.Email)
	if email == "" {
		return errors.New("the person has no email")
	}
	var holders int64
	if err := model.DB.Unscoped().Model(&model.User{}).Where("oidc_id = ? OR username = ?", *person.Sub, email).Count(&holders).Error; err != nil {
		return err
	}
	if holders > 0 {
		return errors.New("another user holds this sub or username")
	}
	if err := model.EnsureEmailAvailable(email, 0); err != nil {
		return err
	}
	displayName, _, _ := strings.Cut(email, "@")
	user := model.User{
		Username:    email,
		DisplayName: displayName[:min(len(displayName), model.UserNameMaxLength)],
		Email:       email,
		OidcId:      *person.Sub,
		Role:        common.RoleCommonUser,
		Status:      userStatus(person),
		Group:       Group,
		Quota:       personQuota,
		AffCode:     common.GetRandomString(16),
		AuthVersion: 1,
	}
	user.SetSetting(dto.UserSetting{AcceptUnsetRatioModel: true})
	return model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		return tx.Create(&records.PersonUser{UserID: user.Id, PersonID: person.ID, Role: person.Role, UpdatedAt: time.Now().UTC()}).Error
	})
}

// updatePersonUser brings the person's row to the snapshot. A change of status, role or
// group raises the user's authentication version and ends every session, so a suspended
// person is signed out at once rather than when their session would end.
func updatePersonUser(link records.PersonUser, person *snapshot.Person) error {
	var user model.User
	if err := model.DB.Where("id = ?", link.UserID).First(&user).Error; err != nil {
		return err
	}
	email := model.NormalizeEmail(person.Email)
	changes := map[string]any{}
	if email != "" && user.Username != email {
		changes["username"] = email
	}
	if email != "" && user.Email != email {
		changes["email"] = email
	}
	if user.OidcId != *person.Sub {
		changes["oidc_id"] = *person.Sub
	}
	if user.Quota < personQuota/2 {
		changes["quota"] = personQuota
	}
	signOut := user.Status != userStatus(person) || user.Role != common.RoleCommonUser || user.Group != Group
	if signOut {
		changes["status"], changes["role"], changes["group"] = userStatus(person), common.RoleCommonUser, Group
	}
	if len(changes) == 0 && link.Role == person.Role {
		return nil
	}
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		if signOut {
			if _, err := model.IncrementUserAuthVersionWithTx(tx, user.Id); err != nil {
				return err
			}
		}
		if len(changes) > 0 {
			if err := tx.Model(&model.User{}).Where("id = ?", user.Id).Updates(changes).Error; err != nil {
				return err
			}
		}
		return tx.Model(&records.PersonUser{}).Where("user_id = ?", user.Id).
			Updates(map[string]any{"role": person.Role, "updated_at": time.Now().UTC()}).Error
	})
	if err != nil {
		return err
	}
	return afterUserChange(user.Id, signOut)
}

func disableUser(userID int) error {
	var user model.User
	err := model.DB.Where("id = ?", userID).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if user.Status == common.UserStatusDisabled {
		return nil
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := model.IncrementUserAuthVersionWithTx(tx, userID); err != nil {
			return err
		}
		return tx.Model(&model.User{}).Where("id = ?", userID).Update("status", common.UserStatusDisabled).Error
	})
	if err != nil {
		return err
	}
	return afterUserChange(userID, true)
}

func afterUserChange(userID int, signOut bool) error {
	if err := model.PublishUserAuthCache(userID); err != nil {
		return err
	}
	if !signOut {
		return nil
	}
	if _, err := model.RevokeAllUserSessions(userID, "essera_person_inactive"); err != nil {
		return err
	}
	// The refresh tokens of the Cognito sign-ins behind those sessions are dropped with them.
	return model.DB.Where("user_id = ?", userID).Delete(&records.CognitoSession{}).Error
}
