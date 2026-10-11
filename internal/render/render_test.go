// 本文件检查渲染结果：VIP、权重、四层限制，以及优先级超过 253 时必须拒绝。
package render

import (
	"fmt"
	"strings"
	"testing"
)

// fixture 是一份能通过校验的最小集群：两台节点、一个七层上游、一个四层上游。
func fixture() Input {
	return Input{
		Cluster: Cluster{
			Name: "edge", VIP: "10.0.0.10", Prefix: 24, VRID: 51,
			Interface: "eth0", AuthPass: "Ab12cd34", AdvertInt: 1,
		},
		Members: []Member{
			{NodeID: "a", NodeName: "lb1", Address: "10.0.0.11", Priority: 150},
			{NodeID: "b", NodeName: "lb2", Address: "10.0.0.12", Priority: 100},
		},
		Upstreams: []Upstream{
			{
				Name: "web", Layer: "http", Method: "least_conn",
				Servers: []Server{
					{Address: "10.1.0.2", Port: 8080, Weight: 1, MaxFails: 3, FailTimeout: 10},
					{Address: "10.1.0.3", Port: 8080, Weight: 2, MaxFails: 3, FailTimeout: 10, Backup: true},
				},
			},
			{
				Name: "db", Layer: "stream", Method: "round_robin",
				Servers: []Server{{Address: "10.1.0.4", Port: 3306, Weight: 1, MaxFails: 3, FailTimeout: 10}},
			},
		},
		Sites: []Site{{
			Name: "app", ListenPort: 443, ServerName: "app.example.com",
			TLS: true, CertName: "app", Upstream: "web", WebSocket: true,
		}},
		Streams: []Stream{{Name: "mysql", Protocol: "tcp", ListenPort: 3306, Upstream: "db"}},
		Certificates: []Certificate{{
			Name: "app", Certificate: "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n",
			PrivateKey: "-----BEGIN PRIVATE KEY-----\nMIIB\n-----END PRIVATE KEY-----\n",
		}},
	}
}

// TestBuild 确认生成的 keepalived 权重是 -150，并且两台节点各有一份配置。
func TestBuild(t *testing.T) {
	bundle, err := Build(fixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Hash) != 64 {
		t.Fatalf("hash %s", bundle.Hash)
	}
	up := file(t, bundle, "", "/etc/nginx/rocketlb/http-upstreams.conf")
	wantUp := header + "# Passive health checks use max_fails. Active checks are not configured.\n\n" +
		"upstream web {\n    least_conn;\n" +
		"    server 10.1.0.2:8080 weight=1 max_fails=3 fail_timeout=10s;\n" +
		"    server 10.1.0.3:8080 weight=2 max_fails=3 fail_timeout=10s backup;\n}\n"
	if up != wantUp {
		t.Fatalf("upstream:\n%s", up)
	}
	site := file(t, bundle, "", "/etc/nginx/rocketlb/sites.conf")
	if !strings.Contains(site, "listen 443 ssl default_server backlog=16384 reuseport;") || !strings.Contains(site, "\n    listen 443 ssl;\n") || !strings.Contains(site, "return 444;") || !strings.Contains(site, "server_name app.example.com;") || !strings.Contains(site, "proxy_pass http://web;") {
		t.Fatalf("site:\n%s", site)
	}
	if strings.Count(site, "default_server") != 1 || strings.Count(site, "reuseport") != 1 {
		t.Fatalf("listen lines:\n%s", site)
	}
	stream := file(t, bundle, "", "/etc/nginx/rocketlb/streams.conf")
	if !strings.Contains(stream, "listen 3306 backlog=16384 reuseport;") || !strings.Contains(stream, "proxy_pass db;") {
		t.Fatalf("stream:\n%s", stream)
	}
	keep := file(t, bundle, "a", "/etc/keepalived/keepalived.conf")
	if !strings.Contains(keep, "weight -150") || !strings.Contains(keep, "chk_lvs") || !strings.Contains(keep, "unicast_src_ip 10.0.0.11") || !strings.Contains(keep, "10.0.0.12") {
		t.Fatalf("keepalived:\n%s", keep)
	}
	if !strings.Contains(keep, "auth_pass Ab12cd34") || !strings.Contains(keep, "10.0.0.10/24") {
		t.Fatalf("keepalived vip:\n%s", keep)
	}
	if !strings.Contains(keep, "lb_kind DR") || !strings.Contains(keep, "real_server 10.0.0.11 443") || !strings.Contains(keep, "real_server 10.0.0.12 3306") {
		t.Fatalf("lvs:\n%s", keep)
	}
	check := file(t, bundle, "a", "/usr/libexec/rocketlb/check-lvs")
	if !strings.Contains(check, "10.0.0.10/24") || !strings.Contains(check, "10.0.0.10:") {
		t.Fatalf("check-lvs:\n%s", check)
	}
	prep := file(t, bundle, "", "/usr/libexec/rocketlb/lvs-prep")
	if !strings.Contains(prep, "ip addr replace 10.0.0.10/32 dev lo") || !strings.Contains(prep, "ipvsadm -C") || !strings.Contains(prep, "ipvsadm -a -t 10.0.0.10:443 -r 10.0.0.12:443") {
		t.Fatalf("lvs-prep:\n%s", prep)
	}
	redacted := bundle.Redacted()
	key := file(t, redacted, "", "/etc/nginx/rocketlb/certs/app.key")
	if strings.Contains(key, "PRIVATE KEY") {
		t.Fatal("private key was not redacted")
	}
}

// TestSplitLayout 确认四台分开部署时，转发节点不跑 nginx，nginx 节点不做转发。
func TestSplitLayout(t *testing.T) {
	in := fixture()
	in.Members = []Member{
		{NodeID: "a", NodeName: "lvs1", Address: "10.0.0.11", Priority: 150, Role: RoleLVS},
		{NodeID: "b", NodeName: "lvs2", Address: "10.0.0.12", Priority: 100, Role: RoleLVS},
		{NodeID: "c", NodeName: "web1", Address: "10.0.0.21", Priority: 100, Role: RoleNginx},
		{NodeID: "d", NodeName: "web2", Address: "10.0.0.22", Priority: 100, Role: RoleNginx},
	}
	bundle, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	keep := file(t, bundle, "a", "/etc/keepalived/keepalived.conf")
	if !strings.Contains(keep, "chk_lvs") || strings.Contains(keep, "chk_nginx") || strings.Contains(keep, "10.0.0.12") == false {
		t.Fatalf("director keepalived:\n%s", keep)
	}
	if strings.Contains(keep, "real_server 10.0.0.11") || !strings.Contains(keep, "real_server 10.0.0.21 443") || !strings.Contains(keep, "real_server 10.0.0.22 3306") {
		t.Fatalf("reals:\n%s", keep)
	}
	if _, err := pick(bundle, "c", "/etc/keepalived/keepalived.conf"); err == nil {
		t.Fatal("nginx node should not get keepalived")
	}
	if _, err := pick(bundle, "c", "/usr/libexec/rocketlb/check-lvs"); err == nil {
		t.Fatal("nginx node should not get the lvs check")
	}
	if !strings.Contains(file(t, bundle, "a", "/usr/libexec/rocketlb/check-lvs"), "10.0.0.10:") {
		t.Fatal("director missing lvs check")
	}
	if _, err := pick(bundle, "a", "/etc/nginx/nginx.conf"); err == nil {
		t.Fatal("director should not get nginx")
	}
	site := file(t, bundle, "c", "/etc/nginx/rocketlb/sites.conf")
	if !strings.Contains(site, "listen 443 ssl") {
		t.Fatalf("nginx site:\n%s", site)
	}
	prep := file(t, bundle, "a", "/usr/libexec/rocketlb/lvs-prep")
	if strings.Contains(prep, "ip addr replace") || !strings.Contains(prep, "ip addr del 10.0.0.10/32 dev lo") || !strings.Contains(prep, "ipvsadm -a -t 10.0.0.10:443 -r 10.0.0.21:443") {
		t.Fatalf("director prep:\n%s", prep)
	}
	real := file(t, bundle, "d", "/usr/libexec/rocketlb/lvs-real")
	if !strings.Contains(real, "ip addr replace 10.0.0.10/32 dev lo") || strings.Contains(real, "ipvsadm") {
		t.Fatalf("real prep:\n%s", real)
	}
}

// TestRejectsSingleNodeAndStreamIPHash 确认单节点不能发布，四层上游不能用 ip_hash。
func TestRejectsSingleNodeAndStreamIPHash(t *testing.T) {
	in := fixture()
	in.Members = in.Members[:1]
	if _, err := Build(in); err == nil {
		t.Fatal("expected member error")
	}
	in = fixture()
	in.Upstreams[1].Method = "ip_hash"
	if _, err := Build(in); err == nil {
		t.Fatal("expected ip_hash error")
	}
}

// TestTrackWeight 确认优先级超过 253 会被拒绝。
func TestTrackWeight(t *testing.T) {
	w, err := TrackWeight([]int{150, 100})
	if err != nil || w != -150 {
		t.Fatalf("weight %d %v", w, err)
	}
	if _, err := TrackWeight([]int{255, 100}); err == nil {
		t.Fatal("expected priority above 253 to be rejected")
	}
}

// file 从渲染结果里取出指定节点、指定路径的内容。
func file(t *testing.T, bundle Bundle, nodeID, path string) string {
	t.Helper()
	body, err := pick(bundle, nodeID, path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func pick(bundle Bundle, nodeID, path string) (string, error) {
	for _, f := range bundle.Files {
		if f.NodeID == nodeID && f.Path == path {
			return f.Content, nil
		}
	}
	return "", fmt.Errorf("missing %s node=%s", path, nodeID)
}
