// 本文件生成 nginx 主配置、上游、站点和四层代理。文件内容最后由 render.Build 收进 Bundle。
package render

import (
	"fmt"
	"strings"
)

// nginxConf 是主配置骨架。具体站点和上游放在 /etc/nginx/rocketlb/，用 include 引进来。
// Ubuntu 20.04、22.04、24.04 的 stream 都是 libnginx-mod-stream 放进 modules-enabled 的动态模块。
// 连接数 16384 与 Kubernetes Ingress Nginx 的生产默认值一致，一个 worker 最多同时持有这么多条连接，含连后端的连接。
// 文件句柄 65535 是企业服务常用的单进程上限，高于连接数，给日志和其他文件留余量。
func nginxConf() string {
	return header + `include /etc/nginx/modules-enabled/*.conf;
user www-data;
worker_processes auto;
pid /run/nginx.pid;
error_log /var/log/nginx/error.log warn;
worker_rlimit_nofile 65535;

events {
    worker_connections 16384;
}

http {
    include /etc/nginx/mime.types;
    default_type application/octet-stream;
    sendfile on;
    server_tokens off;
    keepalive_timeout 65;

    map $http_upgrade $connection_upgrade {
        default upgrade;
        ''      close;
    }

    include /etc/nginx/rocketlb/http-upstreams.conf;
    include /etc/nginx/rocketlb/sites.conf;
}

stream {
    include /etc/nginx/rocketlb/stream-upstreams.conf;
    include /etc/nginx/rocketlb/streams.conf;
}
`
}

// upstreamFile 生成七层或四层的 upstream 块。后端的 max_fails 和 fail_timeout 是开源 nginx 的被动健康检查。
func upstreamFile(layer string, ups []Upstream) string {
	var b strings.Builder
	b.WriteString(header)
	if layer == "http" {
		b.WriteString("# Passive health checks use max_fails. Active checks are not configured.\n")
	}
	if len(ups) == 0 {
		b.WriteString("\n")
		return b.String()
	}
	b.WriteByte('\n')
	for i, up := range ups {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "upstream %s {\n", up.Name)
		b.WriteString(methodLine(up.Method))
		for _, server := range up.Servers {
			fmt.Fprintf(&b, "    server %s:%d weight=%d max_fails=%d fail_timeout=%ds",
				server.Address, server.Port, server.Weight, server.MaxFails, server.FailTimeout)
			if server.Backup {
				b.WriteString(" backup")
			}
			if server.Down {
				b.WriteString(" down")
			}
			b.WriteString(";\n")
		}
		b.WriteString("}\n")
	}
	return b.String()
}

// methodLine 把界面上的算法写成 nginx 指令。空字符串表示默认的轮询，不需要额外一行。
func methodLine(method string) string {
	switch method {
	case "least_conn":
		return "    least_conn;\n"
	case "ip_hash":
		return "    ip_hash;\n"
	case "hash":
		return "    hash $remote_addr consistent;\n"
	default:
		return ""
	}
}

// sitesFile 生成七层 server。每条站点只有 location /，可选 TLS 和 WebSocket 头。backlog 与系统的 somaxconn 同为 16384，nginx 默认只排队 511。
func sitesFile(sites []Site) string {
	var b strings.Builder
	b.WriteString(header)
	if len(sites) == 0 {
		b.WriteByte('\n')
		return b.String()
	}
	b.WriteByte('\n')
	seen := map[string]struct{}{}
	for _, site := range sites {
		key := fmt.Sprintf("%d/%t", site.ListenPort, site.TLS)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		writeDefaultServer(&b, site.ListenPort, site.TLS)
		b.WriteByte('\n')
	}
	for i, site := range sites {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("server {\n")
		// 同一端口只有第一条 listen 能带 backlog 和 reuseport。Ubuntu 20.04 的 nginx 1.18 会把重复参数当成错误。
		if site.TLS {
			fmt.Fprintf(&b, "    listen %d ssl;\n", site.ListenPort)
		} else {
			fmt.Fprintf(&b, "    listen %d;\n", site.ListenPort)
		}
		fmt.Fprintf(&b, "    server_name %s;\n", site.ServerName)
		if site.TLS {
			b.WriteByte('\n')
			fmt.Fprintf(&b, "    ssl_certificate /etc/nginx/rocketlb/certs/%s.crt;\n", site.CertName)
			fmt.Fprintf(&b, "    ssl_certificate_key /etc/nginx/rocketlb/certs/%s.key;\n", site.CertName)
			b.WriteString("    ssl_protocols TLSv1.2 TLSv1.3;\n")
		}
		b.WriteString("\n    location / {\n")
		b.WriteString("        proxy_http_version 1.1;\n")
		b.WriteString("        proxy_set_header Host $host;\n")
		b.WriteString("        proxy_set_header X-Real-IP $remote_addr;\n")
		b.WriteString("        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n")
		b.WriteString("        proxy_set_header X-Forwarded-Proto $scheme;\n")
		if site.WebSocket {
			b.WriteString("        proxy_set_header Upgrade $http_upgrade;\n")
			b.WriteString("        proxy_set_header Connection $connection_upgrade;\n")
		}
		fmt.Fprintf(&b, "        proxy_pass http://%s;\n", site.Upstream)
		b.WriteString("    }\n")
		b.WriteString("}\n")
	}
	return b.String()
}

// writeDefaultServer 接住没有匹配域名的连接。nginx 会把同端口的第一条监听当成默认站点，
// 只用 IP 访问时就会进那一条。这里显式占住默认位置并直接断开。
func writeDefaultServer(b *strings.Builder, port int, tls bool) {
	b.WriteString("server {\n")
	if tls {
		fmt.Fprintf(b, "    listen %d ssl default_server backlog=16384 reuseport;\n", port)
		b.WriteString("    server_name _;\n")
		b.WriteString("    ssl_certificate /etc/nginx/rocketlb/default.crt;\n")
		b.WriteString("    ssl_certificate_key /etc/nginx/rocketlb/default.key;\n")
		b.WriteString("    ssl_protocols TLSv1.2 TLSv1.3;\n")
	} else {
		fmt.Fprintf(b, "    listen %d default_server backlog=16384 reuseport;\n", port)
		b.WriteString("    server_name _;\n")
	}
	b.WriteString("    return 444;\n")
	b.WriteString("}\n")
}

func sitesUseTLS(sites []Site) bool {
	for _, site := range sites {
		if site.TLS {
			return true
		}
	}
	return false
}

// streamsFile 生成四层 server，把监听端口转到对应的 stream 上游。
func streamsFile(streams []Stream) string {
	var b strings.Builder
	b.WriteString(header)
	if len(streams) == 0 {
		b.WriteByte('\n')
		return b.String()
	}
	b.WriteByte('\n')
	for i, stream := range streams {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("server {\n")
		if stream.Protocol == "udp" {
			fmt.Fprintf(&b, "    listen %d udp backlog=16384 reuseport;\n", stream.ListenPort)
		} else {
			fmt.Fprintf(&b, "    listen %d backlog=16384 reuseport;\n", stream.ListenPort)
		}
		fmt.Fprintf(&b, "    proxy_pass %s;\n", stream.Upstream)
		b.WriteString("    proxy_connect_timeout 5s;\n")
		b.WriteString("}\n")
	}
	return b.String()
}

// checkLvs 给 Keepalived 判断这台机器还能不能拿 VIP。拿到 VIP 时，本机必须有 LVS 规则；备机本来就没有规则，所以只确认 ipvsadm 还能用。
func checkLvs(vip string, prefix int) string {
	return fmt.Sprintf(`#!/bin/sh
# Generated by RocketLB. Do not edit on the node.
if ! ipvsadm -Ln >/dev/null 2>&1; then
  exit 1
fi
if ip -4 -o addr show | grep -q ' %s/%d '; then
  ipvsadm -Ln | grep -q '%s:' || exit 1
fi
exit 0
`, vip, prefix, vip)
}
