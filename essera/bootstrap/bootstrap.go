// Package bootstrap prepares new-api to serve the Console's customers: one service user and
// token every authenticated Essera request runs as, in a group whose ratio is zero so new-api
// neither reserves nor charges quota. Pricing is the Console's; limits are the policy's.
package bootstrap

import (
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/configsync"
	"github.com/QuantumNous/new-api/essera/settings"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"gorm.io/gorm"
)

const (
	ServiceUsername  = "essera-service"
	ServiceTokenName = "essera-service"
	serviceQuota     = 2_000_000_000
)

var serviceTokenKey string

// ServiceTokenKey is the key of the token Essera requests run as; never handed out.
func ServiceTokenKey() string { return serviceTokenKey }

// Run makes sure the service user, its token and the group exist, and that new-api keeps no
// second copy of the usage ledger. Only the master node writes; every node reads the token.
func Run() error {
	if common.IsMasterNode {
		if err := ensureOptions(); err != nil {
			return err
		}
		if err := rotateDefaultRootPassword(); err != nil {
			return err
		}
	}
	user, err := ensureServiceUser()
	if err != nil {
		return err
	}
	token, err := ensureServiceToken(user.Id)
	if err != nil {
		return err
	}
	serviceTokenKey = token.Key
	return nil
}

func ensureOptions() error {
	ratios := ratio_setting.GetGroupRatioCopy()
	if ratio, ok := ratios[configsync.Group]; !ok || ratio != 0 {
		ratios[configsync.Group] = 0
		encoded, err := common.Marshal(ratios)
		if err != nil {
			return err
		}
		if err := model.UpdateOption("GroupRatio", string(encoded)); err != nil {
			return err
		}
	}
	usable := setting.GetUserUsableGroupsCopy()
	if _, ok := usable[configsync.Group]; !ok {
		usable[configsync.Group] = "Essera"
		encoded, err := common.Marshal(usable)
		if err != nil {
			return err
		}
		if err := model.UpdateOption("UserUsableGroups", string(encoded)); err != nil {
			return err
		}
	}
	// The name people see is Essera's; the attribution to new-api stays in the footer.
	branding := map[string]string{"SystemName": settings.SystemName(), "Logo": settings.Logo()}
	for option, value := range branding {
		common.OptionMapRWMutex.RLock()
		current := common.OptionMap[option]
		common.OptionMapRWMutex.RUnlock()
		if value == "" || current == value {
			continue
		}
		if err := model.UpdateOption(option, value); err != nil {
			return err
		}
	}
	// The Console's ledger is the record of every request; new-api's would be a second copy.
	for _, option := range []string{"LogConsumeEnabled", "DataExportEnabled"} {
		if err := model.UpdateOption(option, strconv.FormatBool(false)); err != nil {
			return err
		}
	}
	return nil
}

// rotateDefaultRootPassword replaces the well-known password new-api gives the first root user.
func rotateDefaultRootPassword() error {
	var root model.User
	if err := model.DB.Where("role = ?", common.RoleRootUser).First(&root).Error; err != nil {
		return nil
	}
	if !common.ValidatePasswordAndHash("123456", root.Password) {
		return nil
	}
	random, err := common.GenerateRandomKey(32)
	if err != nil {
		return err
	}
	hash, err := common.Password2Hash(random)
	if err != nil {
		return err
	}
	common.SysLog(common.LogText("essera: the default root password was replaced; reset it through the database to sign in"))
	return model.DB.Model(&root).Update("password", hash).Error
}

func ensureServiceUser() (*model.User, error) {
	var user model.User
	err := model.DB.Where("username = ?", ServiceUsername).First(&user).Error
	if err == nil {
		if user.Quota < serviceQuota/2 && common.IsMasterNode {
			// The group ratio is zero, so this only drifts if someone changes the ratio.
			err = model.DB.Model(&user).Update("quota", serviceQuota).Error
		}
		return &user, err
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if !common.IsMasterNode {
		return nil, errors.New("the Essera service user is not created yet")
	}
	random, err := common.GenerateRandomKey(32)
	if err != nil {
		return nil, err
	}
	hash, err := common.Password2Hash(random)
	if err != nil {
		return nil, err
	}
	user = model.User{
		Username:    ServiceUsername,
		Password:    hash,
		DisplayName: "Essera Operations Console",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       configsync.Group,
		Quota:       serviceQuota,
	}
	user.SetSetting(dto.UserSetting{AcceptUnsetRatioModel: true})
	return &user, model.DB.Create(&user).Error
}

func ensureServiceToken(userID int) (*model.Token, error) {
	var token model.Token
	err := model.DB.Where("user_id = ? AND name = ?", userID, ServiceTokenName).First(&token).Error
	if err == nil {
		return &token, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if !common.IsMasterNode {
		return nil, errors.New("the Essera service token is not created yet")
	}
	key, err := common.GenerateKey()
	if err != nil {
		return nil, err
	}
	token = model.Token{
		UserId:         userID,
		Name:           ServiceTokenName,
		Key:            key,
		Status:         common.TokenStatusEnabled,
		CreatedTime:    common.GetTimestamp(),
		AccessedTime:   common.GetTimestamp(),
		ExpiredTime:    -1,
		UnlimitedQuota: true,
		Group:          configsync.Group,
	}
	return &token, token.Insert()
}
