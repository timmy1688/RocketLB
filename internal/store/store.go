// Package store 打开数据库并保持表结构跟代码一致。
// 开发和生产用 Postgres；测试用临时 SQLite，避免每次跑测试都要一台数据库。
package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"rocketlb/internal/config"
	"rocketlb/internal/model"

	sqlite "github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
)

// Open 按配置连接数据库。SQLite 只开一条连接并打开外键，Postgres 用驱动自己的连接池。
func Open(cfg config.Config) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch cfg.DBDriver {
	case "sqlite":
		if !strings.Contains(cfg.DBDSN, ":memory:") {
			if err := os.MkdirAll(filepath.Dir(cfg.DBDSN), 0o755); err != nil {
				return nil, err
			}
		}
		dialector = sqlite.Open(cfg.DBDSN)
	case "postgres":
		dialector = postgres.Open(cfg.DBDSN)
	default:
		return nil, fmt.Errorf("unsupported database driver %s", cfg.DBDriver)
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	if cfg.DBDriver == "sqlite" {
		sqlDB.SetMaxOpenConns(1)
		if err := db.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
			return nil, err
		}
		if err := db.Exec("PRAGMA busy_timeout = 5000").Error; err != nil {
			return nil, err
		}
	}
	return db, nil
}

// Migrate 每次启动都跑。先把旧库里 GORM 自动拆开的 v_ip、vr_id 改回 vip、vrid，再建缺失的表和列。
func Migrate(db *gorm.DB) error {
	if err := renameColumn(db, "clusters", "v_ip", "vip"); err != nil {
		return err
	}
	if err := renameColumn(db, "clusters", "vr_id", "vrid"); err != nil {
		return err
	}
	return db.AutoMigrate(
		&model.User{},
		&model.Node{},
		&model.Cluster{},
		&model.ClusterMember{},
		&model.Upstream{},
		&model.UpstreamServer{},
		&model.Site{},
		&model.Stream{},
		&model.Certificate{},
		&model.Revision{},
		&model.DeployJob{},
	)
}

// renameColumn 只在旧列还在、新列还不在时改名。新库两列都不存在，这里直接跳过。
func renameColumn(db *gorm.DB, table, from, to string) error {
	if !db.Migrator().HasTable(table) {
		return nil
	}
	if !db.Migrator().HasColumn(table, from) || db.Migrator().HasColumn(table, to) {
		return nil
	}
	return db.Migrator().RenameColumn(table, from, to)
}
