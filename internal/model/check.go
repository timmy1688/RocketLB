package model

import (
	"errors"
	"net"
	"regexp"
	"strconv"
	"strings"
)

// 这些规则挡在写库和渲染之前，避免非法名字进 nginx 或 keepalived。
// 返回的是给界面看的中文句子，调用方再包成 400 错误。
var (
	nameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,31}$`)
	hostRe = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$`)
	userRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,31}$`)
	nicRe  = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,15}$`)
	passRe = regexp.MustCompile(`^[A-Za-z0-9]{1,8}$`)
	dnsRe  = regexp.MustCompile(`^[A-Za-z0-9*](?:[A-Za-z0-9_.-]{0,251}[A-Za-z0-9*])?$`)
)

// CheckName 检查集群、上游、站点这类标识。必须以字母开头，避免和 nginx 关键字或数字端口搞混。
func CheckName(s string) error {
	if !nameRe.MatchString(s) {
		return errors.New("名称需以字母开头，只能包含字母、数字、下划线和短横线，最长 32 个字符")
	}
	return nil
}

// CheckIPv4 检查节点地址和 VIP。VRRP 单播源地址必须是 IP，主机名在这里不合格。
func CheckIPv4(s string) error {
	ip := net.ParseIP(s)
	if ip == nil || ip.To4() == nil {
		return errors.New("需要 IPv4 地址")
	}
	return nil
}

// CheckHost 检查后端地址。后端可以是 IP，也可以是主机名。
func CheckHost(s string) error {
	if err := CheckIPv4(s); err == nil {
		return nil
	}
	if hostRe.MatchString(s) && len(s) <= 253 {
		return nil
	}
	return errors.New("后端地址需为 IPv4 或主机名")
}

// CheckPort 检查监听端口和后端端口。
func CheckPort(n int) error {
	if n < 1 || n > 65535 {
		return errors.New("端口需在 1 到 65535 之间")
	}
	return nil
}

// CheckSSHUser 检查登录节点用的用户名，安装时这个用户需要能 sudo。
func CheckSSHUser(s string) error {
	if !userRe.MatchString(s) {
		return errors.New("SSH 用户名不合法")
	}
	return nil
}

// CheckInterface 检查网卡名。同一个集群的节点要用同名网卡承载 VIP，例如 ens33。
func CheckInterface(s string) error {
	if !nicRe.MatchString(s) {
		return errors.New("网卡名不合法")
	}
	return nil
}

// CheckAuthPass 检查 Keepalived 的 VRRP 认证串。协议只允许 1 到 8 个字母或数字。
func CheckAuthPass(s string) error {
	if !passRe.MatchString(s) {
		return errors.New("Keepalived 认证密码只能是 1 到 8 位字母或数字")
	}
	return nil
}

// CheckServerName 检查站点域名，多个域名用空格分开，允许 *.example.com 这种通配。
func CheckServerName(s string) error {
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return errors.New("需要填写域名")
	}
	for _, part := range parts {
		if !dnsRe.MatchString(part) || len(part) > 253 {
			return errors.New("域名包含无法写入配置的字符")
		}
	}
	return nil
}

// CheckPrefix 检查 VIP 的掩码长度，例如 24 表示 255.255.255.0。
func CheckPrefix(n int) error {
	if n < 1 || n > 32 {
		return errors.New("VIP 前缀需在 1 到 32 之间")
	}
	return nil
}

// CheckVRID 是同一网段里区分不同 VRRP 组的编号，1 到 255。
func CheckVRID(n int) error {
	if n < 1 || n > 255 {
		return errors.New("VRID 需在 1 到 255 之间")
	}
	return nil
}

// CheckPriority 检查节点优先级。渲染时还会再限制到 253，因为健康检查的权重不能低于 -253。
func CheckPriority(n int) error {
	if n < 1 || n > 255 {
		return errors.New("优先级需在 1 到 255 之间")
	}
	return nil
}

// ListenConflict 是站点和四层代理抢同一个端口时给用户看的句子。
func ListenConflict(port int, protocol string) string {
	return "端口 " + strconv.Itoa(port) + "/" + protocol + " 已被占用"
}
