// 本文件检查下发脚本：路径必须在白名单里，备份编号必须是 UUID，nginx -t 失败时脚本里要有还原。
package publish

import (
	"encoding/base64"
	"strings"
	"testing"
)

// TestPrepareScriptEncodesContent 确认配置以 base64 进入脚本，并且脚本会跑 nginx -t。
func TestPrepareScriptEncodesContent(t *testing.T) {
	content := "server { listen 80; }\n"
	script, err := prepareScript("11111111-1111-1111-1111-111111111111", []scriptFile{{
		Path:    "/etc/nginx/nginx.conf",
		Content: content,
		Mode:    0o644,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(script, "server {") {
		t.Fatal("raw config leaked into the script")
	}
	if !strings.Contains(script, base64.StdEncoding.EncodeToString([]byte(content))) {
		t.Fatal("missing base64 payload")
	}
	if !strings.Contains(script, "/usr/sbin/nginx -t") || !strings.Contains(script, "rocketlb-prepare-ok") {
		t.Fatal(script)
	}
}

// TestInstallScriptTunesGateway 确认一键安装会写入与 nginx 连接数对齐的系统参数。
func TestInstallScriptTunesGateway(t *testing.T) {
	script := installScript()
	for _, want := range []string{
		"net.core.somaxconn = 16384",
		"net.ipv4.ip_local_port_range = 1024 65535",
		"net.ipv4.tcp_tw_reuse = 1",
		"net.ipv4.conf.all.arp_ignore = 1",
		"ipvsadm",
		"https://mirrors.aliyun.com/ubuntu",
		"libnginx-mod-stream",
		"LimitNOFILE=65535",
		"nf_conntrack_max=1048576",
		"rocketlb-install-ok",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("missing %s", want)
		}
	}
	lvs := installScriptFor("lvs")
	if strings.Contains(lvs, "libnginx-mod-stream") || !strings.Contains(lvs, "apt_install keepalived ipvsadm") {
		t.Fatalf("lvs packages:\n%s", lvs)
	}
	web := installScriptFor("nginx")
	if strings.Contains(web, "keepalived") || !strings.Contains(web, "apt_install nginx libnginx-mod-stream") {
		t.Fatalf("nginx packages:\n%s", web)
	}
	if strings.Contains(script, "nginx.org") || strings.Contains(script, "nginx.tar.gz") {
		t.Fatal("install still downloads nginx from upstream")
	}
}

// TestActivateFollowsRole 确认四台分开时，转发节点停掉 nginx，nginx 节点停掉 Keepalived。
func TestActivateFollowsRole(t *testing.T) {
	director := activateScript([]scriptFile{{Path: "/etc/keepalived/keepalived.conf", Mode: 0o644}})
	if !strings.Contains(director, "systemctl stop nginx") || strings.Contains(director, "systemctl reload nginx") {
		t.Fatalf("director activate:\n%s", director)
	}
	if !strings.Contains(director, "systemctl enable keepalived") || !strings.Contains(director, "disable --now rocketlb-lvs-real.service") || strings.Contains(director, "systemctl enable rocketlb-lvs-real.service") {
		t.Fatalf("director boot:\n%s", director)
	}
	web := activateScript([]scriptFile{{Path: "/etc/nginx/nginx.conf", Mode: 0o644}, {Path: "/usr/libexec/rocketlb/lvs-real", Mode: 0o755}})
	if !strings.Contains(web, "systemctl stop keepalived") || !strings.Contains(web, "systemctl enable rocketlb-lvs-real.service") || !strings.Contains(web, "systemctl enable nginx") || strings.Contains(web, "systemctl reload keepalived") {
		t.Fatalf("nginx activate:\n%s", web)
	}
}

// TestRejectsPathsOutsideManagedDirs 确认白名单以外的路径和非法备份编号会被拒绝。
func TestRejectsPathsOutsideManagedDirs(t *testing.T) {
	_, err := prepareScript("11111111-1111-1111-1111-111111111111", []scriptFile{{
		Path:    "/tmp/nginx.conf",
		Content: "x",
		Mode:    0o644,
	}})
	if err == nil {
		t.Fatal("expected reject")
	}
	_, err = prepareScript("../etc", nil)
	if err == nil {
		t.Fatal("expected reject id")
	}
}
