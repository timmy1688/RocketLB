// Package validate 在保存和渲染前检查字段。规则写在这里，是为了不把非法名字写进 nginx 或 keepalived。
// 返回的是给界面看的中文句子，调用方再包成 400 错误。
package validate

import (
	"errors"
	"net"
	"regexp"
	"strconv"
	"strings"
)

var (
	nameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,31}$`)
	hostRe = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$`)
	userRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,31}$`)
	nicRe  = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,15}$`)
	passRe = regexp.MustCompile(`^[A-Za-z0-9]{1,8}$`)
	dnsRe  = regexp.MustCompile(`^[A-Za-z0-9*](?:[A-Za-z0-9_.-]{0,251}[A-Za-z0-9*])?$`)
)

// Name 检查集群、上游、站点这类标识。必须以字母开头，避免和 nginx 关键字或数字端口搞混。
func Name(s string) error {
	if !nameRe.MatchString(s) {
		return errors.New("名称需以字母开头，只能包含字母、数字、下划线和短横线，最长 32 个字符")
	}
	return nil
}

// IPv4 检查节点地址和 VIP。VRRP 单播源地址必须是 IP，主机名在这里不合格。
func IPv4(s string) error {
	ip := net.ParseIP(s)
	if ip == nil || ip.To4() == nil {
		return errors.New("需要 IPv4 地址")
	}
	return nil
}

// Host 检查后端地址。后端可以是 IP，也可以是主机名。
func Host(s string) error {
	if err := IPv4(s); err == nil {
		return nil
	}
	if hostRe.MatchString(s) && len(s) <= 253 {
		return nil
	}
	return errors.New("后端地址需为 IPv4 或主机名")
}

// Port 检查监听端口和后端端口。
func Port(n int) error {
	if n < 1 || n > 65535 {
		return errors.New("端口需在 1 到 65535 之间")
	}
	return nil
}

// SSHUser 检查登录节点用的用户名，安装时这个用户需要能 sudo。
func SSHUser(s string) error {
	if !userRe.MatchString(s) {
		return errors.New("SSH 用户名不合法")
	}
	return nil
}

// Interface 检查网卡名。同一个集群的节点要用同名网卡承载 VIP，例如 ens33。
func Interface(s string) error {
	if !nicRe.MatchString(s) {
		return errors.New("网卡名不合法")
	}
	return nil
}

// AuthPass 检查 Keepalived 的 VRRP 认证串。协议只允许 1 到 8 个字母或数字。
func AuthPass(s string) error {
	if !passRe.MatchString(s) {
		return errors.New("Keepalived 认证密码只能是 1 到 8 位字母或数字")
	}
	return nil
}

// ServerName 检查站点域名，多个域名用空格分开，允许 *.example.com 这种通配。
func ServerName(s string) error {
	if s == "" {
		return errors.New("需要填写域名")
	}
	for _, part := range splitFields(s) {
		if !dnsRe.MatchString(part) || len(part) > 253 {
			return errors.New("域名包含无法写入配置的字符")
		}
	}
	return nil
}

// Prefix 检查 VIP 的掩码长度，例如 24 表示 255.255.255.0。
func Prefix(n int) error {
	if n < 1 || n > 32 {
		return errors.New("VIP 前缀需在 1 到 32 之间")
	}
	return nil
}

// VRID 是同一网段里区分不同 VRRP 组的编号，1 到 255。
func VRID(n int) error {
	if n < 1 || n > 255 {
		return errors.New("VRID 需在 1 到 255 之间")
	}
	return nil
}

// Priority 检查节点优先级。渲染时还会再限制到 253，因为健康检查的权重不能低于 -253。
func Priority(n int) error {
	if n < 1 || n > 255 {
		return errors.New("优先级需在 1 到 255 之间")
	}
	return nil
}

// splitFields 按空白把多个域名拆开。
func splitFields(s string) []string {
	return regexp.MustCompile(`\s+`).Split(s, -1)
}

// ListenConflict 是站点和四层代理抢同一个端口时给用户看的句子。四层没有域名，端口只能有一条。
func ListenConflict(port int, protocol string) string {
	return "端口 " + strconv.Itoa(port) + "/" + protocol + " 已被占用"
}

// OverlappingName 判断两组域名能不能写在同一个端口上。完全相同，或通配符盖住另一边，都算冲突。
// 返回值是 a 里撞上的那一个名字。
func OverlappingName(a, b string) (string, bool) {
	for _, left := range splitFields(a) {
		for _, right := range splitFields(b) {
			if left == "" || right == "" {
				continue
			}
			if nameConflict(strings.ToLower(left), strings.ToLower(right)) {
				return left, true
			}
		}
	}
	return "", false
}

func nameConflict(a, b string) bool {
	if a == b {
		return true
	}
	if suffix, ok := wildcardSuffix(a); ok && coveredByWildcard(b, suffix) {
		return true
	}
	if suffix, ok := wildcardSuffix(b); ok && coveredByWildcard(a, suffix) {
		return true
	}
	return false
}

func wildcardSuffix(name string) (string, bool) {
	if strings.HasPrefix(name, "*.") && len(name) > 2 {
		return name[2:], true
	}
	return "", false
}

func coveredByWildcard(name, suffix string) bool {
	return name == suffix || strings.HasSuffix(name, "."+suffix)
}
