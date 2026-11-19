// Package recordstest gives a test its own database with the Essera tables: PostgreSQL, the
// database the fork runs on, when ESSERA_TEST_POSTGRES_DSN names one; SQLite in memory
// otherwise.
package recordstest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/essera/records"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// UseDatabase points model.DB at a fresh database for the test and migrates the Essera tables
// and the new-api tables named.
func UseDatabase(t *testing.T, tables ...any) {
	t.Helper()
	dialector := open(t)
	previous := model.DB
	t.Cleanup(func() { model.DB = previous })
	connect(t, dialector)
	if len(tables) > 0 {
		require.NoError(t, model.DB.AutoMigrate(tables...))
	}
	reopen.Store(t, dialector)
}

// Restart closes the test's database and opens it again, as a Gateway restart would, and runs
// the Essera migrations again: only what the database kept is left.
func Restart(t *testing.T) {
	t.Helper()
	dialector, ok := reopen.Load(t)
	require.True(t, ok, "UseDatabase first")
	connection, err := model.DB.DB()
	require.NoError(t, err)
	require.NoError(t, connection.Close())
	connect(t, dialector.(gorm.Dialector))
}

var reopen sync.Map // *testing.T → gorm.Dialector

func connect(t *testing.T, dialector gorm.Dialector) {
	db, err := gorm.Open(dialector, &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	require.NoError(t, records.Migrate())
}

func open(t *testing.T) gorm.Dialector {
	dsn := strings.TrimSpace(os.Getenv("ESSERA_TEST_POSTGRES_DSN"))
	if dsn == "" {
		return sqlite.Open(filepath.Join(t.TempDir(), "gateway.db"))
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	schema := fmt.Sprintf("essera_test_%d", time.Now().UnixNano())
	require.NoError(t, admin.Exec("CREATE SCHEMA "+schema).Error)
	t.Cleanup(func() { admin.Exec("DROP SCHEMA " + schema + " CASCADE") })
	return postgres.Open(dsn + " search_path=" + schema)
}
