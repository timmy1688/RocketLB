package cluster

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"rocketlb/internal/config"
	"rocketlb/internal/httpx"
	"rocketlb/internal/model"
	"rocketlb/internal/secret"
	"rocketlb/internal/store"
)

func TestPublishedClusterFreezesTopology(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(config.Config{DBDriver: "sqlite", DBDSN: filepath.Join(t.TempDir(), "t.db")})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	box, err := secret.New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, box)
	n1 := model.Node{ID: model.NewID(), Name: "lb1", Address: "10.0.0.1", SSHPort: 22, SSHUser: "root", AuthType: "password", Status: "unknown"}
	n2 := model.Node{ID: model.NewID(), Name: "lb2", Address: "10.0.0.2", SSHPort: 22, SSHUser: "root", AuthType: "password", Status: "unknown"}
	if err := db.Create(&n1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&n2).Error; err != nil {
		t.Fatal(err)
	}
	members := []MemberInput{{NodeID: n1.ID, Priority: 150}, {NodeID: n2.ID, Priority: 100}}
	created, err := svc.Create(ctx, Write{
		Name: "edge", VIP: "10.0.0.10", Interface: "eth0", Members: members,
	})
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := svc.Update(ctx, created.ID, Write{
		Name: "edge-draft", VIP: created.VIP, Prefix: created.Prefix, VRID: created.VRID,
		Interface: created.Interface, AdvertInt: created.AdvertInt, Layout: created.Layout, Members: members,
	})
	if err != nil || renamed.Name != "edge-draft" {
		t.Fatalf("draft rename: %v %#v", err, renamed.Name)
	}
	if err := db.Create(&model.Revision{
		ID: model.NewID(), ClusterID: created.ID, Hash: "abc", Status: model.RevisionPublished,
	}).Error; err != nil {
		t.Fatal(err)
	}
	_, err = svc.Update(ctx, created.ID, Write{
		Name: "edge-draft", VIP: created.VIP, Prefix: created.Prefix, VRID: created.VRID,
		Interface: created.Interface, AdvertInt: created.AdvertInt, Layout: created.Layout,
		Members: []MemberInput{{NodeID: n1.ID, Priority: 150}},
	})
	var httpErr *httpx.Error
	if !errors.As(err, &httpErr) || httpErr.Status != 409 {
		t.Fatalf("member change: %v", err)
	}
	kept, err := svc.Update(ctx, created.ID, Write{
		Name: "edge-live", VIP: created.VIP, Prefix: created.Prefix, VRID: created.VRID,
		Interface: created.Interface, AdvertInt: created.AdvertInt, Layout: created.Layout, Members: members,
	})
	if err != nil || kept.Name != "edge-live" || len(kept.Members) != 2 {
		t.Fatalf("name only: %v %#v", err, kept)
	}
}
