// Package recordstest gives a test its own database with the Essera tables: PostgreSQL, the
// database the fork runs on, when ESSERA_TEST_POSTGRES_DSN names one; SQLite in memory
// otherwise.
package recordstest

import (
	"fmt"
	"os"
	"strings"
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
	db := open(t)
	previous := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previous })
	require.NoError(t, records.Migrate())
	if len(tables) > 0 {
		require.NoError(t, db.AutoMigrate(tables...))
	}
}

func open(t *testing.T) *gorm.DB {
	dsn := strings.TrimSpace(os.Getenv("ESSERA_TEST_POSTGRES_DSN"))
	if dsn == "" {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		return db
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	schema := fmt.Sprintf("essera_test_%d", time.Now().UnixNano())
	require.NoError(t, admin.Exec("CREATE SCHEMA "+schema).Error)
	t.Cleanup(func() { admin.Exec("DROP SCHEMA " + schema + " CASCADE") })
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{})
	require.NoError(t, err)
	return db
}
