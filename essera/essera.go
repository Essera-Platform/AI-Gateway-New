// Package essera connects this Gateway to the Essera Operations Console: the Console
// administers (providers, customers, entitlements, limits, settings) and publishes immutable
// configuration snapshots; the Gateway applies them, authenticates customers, enforces the
// policy, reports usage and reports its state. See CHANGES-ESSERA.md.
package essera

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/bootstrap"
	"github.com/QuantumNous/new-api/essera/configsync"
	"github.com/QuantumNous/new-api/essera/keys"
	"github.com/QuantumNous/new-api/essera/records"
	"github.com/QuantumNous/new-api/essera/secrets"
	"github.com/QuantumNous/new-api/essera/settings"
	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/QuantumNous/new-api/essera/stats"
	"github.com/QuantumNous/new-api/essera/usage"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

// Start prepares the integration; it must run after the database is open and before the
// node sync starts, because it registers a sync topic. It does nothing unless ESSERA_MANAGED.
func Start(ctx context.Context) error {
	if !settings.Managed() {
		return nil
	}
	model.ResolveChannelKey = secrets.Resolve
	if common.IsMasterNode {
		if err := records.Migrate(); err != nil {
			return err
		}
	}
	if err := bootstrap.Run(); err != nil {
		return err
	}
	if err := configsync.LoadLatest(ctx); err != nil {
		common.SysError(common.LogText("essera: loading the last configuration: %s", err.Error()))
	}
	if common.IsMasterNode {
		if err := keys.Project(snapshot.Current(), time.Now()); err != nil {
			common.SysError(common.LogText("essera: projecting the API keys: %s", err.Error()))
		}
	}
	service.RegisterSyncTopic(configsync.SyncTopic, configsync.LoadLatest)
	go stats.Run(ctx)
	if !settings.Enabled() {
		common.SysLog(common.LogText("essera: managed, but ESSERA_ENVIRONMENT is not set; not connecting to the Console"))
		return nil
	}
	if common.IsMasterNode {
		go configsync.Consume(ctx)
		go usage.Send(ctx)
	}
	common.SysLog(common.LogText("essera: connected to the Console's %s environment", settings.Environment()))
	return nil
}
