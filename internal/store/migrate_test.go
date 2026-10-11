// 本文件确认旧库里的 v_ip、vr_id 会在启动迁移时改回 vip、vrid。
package store

import (
	"path/filepath"
	"testing"

	"rocketlb/internal/config"
	"rocketlb/internal/model"
)

// TestRenameLegacyClusterColumns 先建旧列名，再跑 Migrate，确认数据还在 vip、vrid 上。
func TestRenameLegacyClusterColumns(t *testing.T) {
	db, err := Open(config.Config{
		DBDriver: "sqlite",
		DBDSN:    filepath.Join(t.TempDir(), "rocketlb.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.Exec(`CREATE TABLE clusters (
		id text primary key,
		name text not null,
		v_ip text not null,
		prefix integer not null,
		vr_id integer not null,
		interface_name text not null,
		auth_pass_enc text not null,
		advert_int integer not null,
		created_at datetime,
		updated_at datetime
	)`).Error
	if err != nil {
		t.Fatal(err)
	}
	err = db.Exec(`INSERT INTO clusters
		(id, name, v_ip, prefix, vr_id, interface_name, auth_pass_enc, advert_int)
		VALUES ('c1', 'edge', '10.0.0.10', 24, 51, 'eth0', 'enc', 1)`).Error
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	var row model.Cluster
	if err := db.First(&row, "id = ?", "c1").Error; err != nil {
		t.Fatal(err)
	}
	if row.VIP != "10.0.0.10" || row.VRID != 51 || row.Name != "edge" {
		t.Fatalf("legacy row = %+v", row)
	}
}
