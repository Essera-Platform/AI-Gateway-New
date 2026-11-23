// Essera, 2026-11: the channels a retry could still reach.
package model

import (
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// HasUntriedChannel reports whether an enabled channel of the group serves the
// model, passes the request's channel filters and is not among tried, finding
// channels the way selection does: the exact model name first, then its routing
// name. A failed lookup answers true, so the caller keeps its usual retries.
func HasUntriedChannel(group string, modelName string, tried []int, filters []dto.ChannelFilter) bool {
	var candidates []int
	if common.MemoryCacheEnabled {
		channelSyncLock.RLock()
		candidates, _ = filterCandidateIDs(group2model2channels[group][modelName], modelName, filters)
		if normalized := ratio_setting.RoutingMatchModelName(modelName); len(candidates) == 0 && normalized != "" {
			candidates, _ = filterCandidateIDs(group2model2channels[group][normalized], modelName, filters)
		}
		candidates = slices.Clone(candidates)
		channelSyncLock.RUnlock()
	} else {
		for _, name := range []string{modelName, ratio_setting.RoutingMatchModelName(modelName)} {
			if name == "" || len(candidates) > 0 {
				continue
			}
			var abilities []Ability
			if err := DB.Where(&Ability{Group: group, Model: name, Enabled: true}).Find(&abilities).Error; err != nil {
				return true
			}
			for _, ability := range filterAbilitiesByConstraints(abilities, modelName, filters) {
				candidates = append(candidates, ability.ChannelId)
			}
		}
	}
	return slices.ContainsFunc(candidates, func(id int) bool { return !slices.Contains(tried, id) })
}
