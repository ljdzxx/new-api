package model

import (
	"context"
	"database/sql/driver"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/rogpeppe/go-internal/lockedfile"
	"gorm.io/gorm"
)

// All roles migrate. Serialize the entire check/DDL/backfill sequence for nodes
// sharing a database, including when the main and log DSNs point to the same DB.
func migrateAtStartup(db *gorm.DB, migrate func() error) error {
	pool, err := db.DB()
	if err != nil {
		return err
	}
	pool.SetMaxIdleConns(common.GetEnvOrDefault("SQL_MAX_IDLE_CONNS", 100))
	pool.SetConnMaxLifetime(time.Second * time.Duration(common.GetEnvOrDefault("SQL_MAX_LIFETIME", 60)))
	// The session lock needs one connection in addition to the migration's.
	// Apply the configured pool limit (which may be one) after startup migration.
	defer pool.SetMaxOpenConns(common.GetEnvOrDefault("SQL_MAX_OPEN_CONNS", 1000))

	if db.Dialector.Name() == "sqlite" {
		var path string
		if err := db.Raw("SELECT file FROM pragma_database_list WHERE name = 'main'").Row().Scan(&path); err != nil {
			return err
		}
		if path != "" { // In-memory databases are local to the process.
			unlock, err := lockedfile.MutexAt(path + ".migration.lock").Lock()
			if err != nil {
				return fmt.Errorf("lock SQLite migration: %w", err)
			}
			defer unlock()
		}
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		conn, err := pool.Conn(ctx)
		if err != nil {
			return err
		}
		// Closing the physical session releases its advisory lock, even if a
		// failed acquisition left the session state unknown. Never pool it.
		defer conn.Raw(func(any) error { return driver.ErrBadConn })
		switch db.Dialector.Name() {
		case "mysql":
			var acquired int
			if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(SHA2(CONCAT('new-api:migrate:', DATABASE()), 256), 120)").Scan(&acquired); err != nil {
				return fmt.Errorf("lock MySQL migration: %w", err)
			}
			if acquired != 1 {
				return fmt.Errorf("timed out waiting for MySQL migration lock")
			}
		case "postgres":
			if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock(hashtext('new-api'), hashtext('schema-migration'))"); err != nil {
				return fmt.Errorf("lock PostgreSQL migration: %w", err)
			}
		default:
			return fmt.Errorf("unsupported migration database %q", db.Dialector.Name())
		}
	}
	common.SysLog("database migration started")
	return migrate()
}
