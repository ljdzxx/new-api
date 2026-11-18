package model

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// Use separate processes: startup sets global DB handles and node settings.
func TestStartupMigrationBothRoles(t *testing.T) {
	if dir := os.Getenv("NEW_API_TEST_MIGRATION_DIR"); dir != "" {
		common.IsMasterNode = os.Getenv("NODE_TYPE") != "slave"
		t.Setenv("SQL_DSN", "local")
		t.Setenv("LOG_SQL_DSN", "local")
		common.SQLitePath = filepath.Join(dir, "main.db")
		require.NoError(t, InitDB())
		m := DB.Migrator()
		require.True(t, m.HasTable(&EpayCheckoutSlot{}))
		require.True(t, m.HasIndex(&TopUp{}, "idx_epay_topup_expiry"))
		require.True(t, m.HasIndex(&SubscriptionOrder{}, "idx_epay_sub_expiry"))
		common.SQLitePath = filepath.Join(dir, "log.db")
		require.NoError(t, InitLogDB())
		require.True(t, LOG_DB.Migrator().HasTable(&Log{}))
		return
	}
	dir := t.TempDir()
	run := func(t *testing.T, role string) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestStartupMigrationBothRoles$")
		cmd.Env = append(os.Environ(), "NEW_API_TEST_MIGRATION_DIR="+dir, "NODE_TYPE="+role)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
	}
	run(t, "slave") // A slave can bootstrap both databases before any master.
	for _, role := range []string{"master", "slave"} {
		t.Run(role, func(t *testing.T) { t.Parallel(); run(t, role) })
	}
}
