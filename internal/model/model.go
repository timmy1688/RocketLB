// Package model 是数据库里的表，以及这些字段在写入前的检查。GORM 会按这些结构建表和读写行。
// 给浏览器看的字段不在这里定义，各业务包有自己的 View，避免把密码哈希和加密私钥直接返回。
package model

import (
	"time"

	"github.com/google/uuid"
)

// 这些常量是写进数据库的状态字符串。改名字会影响已有数据，所以和界面文案分开。
const (
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleViewer   = "viewer"

	NodeUnknown = "unknown"
	NodeOnline  = "online"
	NodeOffline = "offline"

	AuthPassword = "password"
	AuthKey      = "key"

	LayerHTTP   = "http"
	LayerStream = "stream"

	JobRunning   = "running"
	JobSucceeded = "succeeded"
	JobFailed    = "failed"

	RevisionPublishing = "publishing"
	RevisionPublished  = "published"
	RevisionFailed     = "failed"

	ActionPublish   = "publish"
	ActionRollback  = "rollback"
	ActionProvision = "provision"

	// 两台一起：每台机器同时转发和跑 nginx。四台分开：一部分机器只转发，一部分只跑 nginx。
	LayoutCombined = "combined"
	LayoutSplit    = "split"
	MemberBoth     = "both"
	MemberLVS      = "lvs"
	MemberNginx    = "nginx"
)

// NewID 生成主键。用字符串形式的 UUID，SQLite 和 Postgres 都能存，也不依赖数据库自增。
func NewID() string { return uuid.NewString() }

// User 是控制面登录账号。PasswordHash 是 bcrypt 结果，不会出现在 JSON 里。
type User struct {
	ID           string `gorm:"primaryKey;size:36"`
	Username     string `gorm:"uniqueIndex;size:64;not null"`
	PasswordHash string `gorm:"size:128;not null" json:"-"`
	Role         string `gorm:"size:16;not null"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Node 是一台将要运行 nginx 和 keepalived 的机器。Address 必须是 IPv4，发布时当作 VRRP 单播地址。
// SecretEnc 是加密后的 SSH 密码或私钥，HostKey 是第一次连接时记住的主机公钥。
type Node struct {
	ID          string `gorm:"primaryKey;size:36"`
	Name        string `gorm:"uniqueIndex;size:32;not null"`
	Address     string `gorm:"size:64;not null"`
	SSHPort     int    `gorm:"not null"`
	SSHUser     string `gorm:"size:32;not null"`
	AuthType    string `gorm:"size:16;not null"`
	SecretEnc   string `gorm:"type:text" json:"-"`
	HostKey     string `gorm:"type:text" json:"-"`
	OS          string `gorm:"size:128"`
	Status      string `gorm:"size:16;not null"`
	LastError   string `gorm:"size:512"`
	LastProbeAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Cluster 是一组共用一个 VIP 的节点。VIP、VRID 用显式列名，避免 GORM 把缩写拆成 v_ip、vr_id。
type Cluster struct {
	ID            string `gorm:"primaryKey;size:36"`
	Name          string `gorm:"uniqueIndex;size:32;not null"`
	VIP           string `gorm:"column:vip;size:64;not null"`
	Prefix        int    `gorm:"not null"`
	VRID          int    `gorm:"column:vrid;not null"`
	InterfaceName string `gorm:"size:32;not null"`
	AuthPassEnc   string `gorm:"type:text;not null" json:"-"`
	AdvertInt     int    `gorm:"not null"`
	Layout        string `gorm:"size:16;not null;default:combined"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ClusterMember 把节点放进集群，并记下 Keepalived 优先级。数字大的节点在两边都健康时持有 VIP。
type ClusterMember struct {
	ID        string `gorm:"primaryKey;size:36"`
	ClusterID string `gorm:"size:36;not null;uniqueIndex:idx_cluster_node"`
	NodeID    string `gorm:"size:36;not null;uniqueIndex:idx_cluster_node;index"`
	Priority  int    `gorm:"not null"`
	Role      string `gorm:"size:16;not null;default:both"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Upstream 是一组后端。Layer 为 http 时给七层站点用，为 stream 时给四层代理用，两边不能混用。
type Upstream struct {
	ID        string           `gorm:"primaryKey;size:36"`
	ClusterID string           `gorm:"size:36;not null;uniqueIndex:idx_upstream_name;index"`
	Name      string           `gorm:"size:32;not null;uniqueIndex:idx_upstream_name"`
	Layer     string           `gorm:"size:16;not null"`
	Method    string           `gorm:"size:32;not null"`
	Servers   []UpstreamServer `gorm:"foreignKey:UpstreamID"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// UpstreamServer 是上游里的一台后端。MaxFails 和 FailTimeout 会写成 nginx 的被动健康检查。
type UpstreamServer struct {
	ID          string `gorm:"primaryKey;size:36"`
	UpstreamID  string `gorm:"size:36;not null;index"`
	Address     string `gorm:"size:128;not null"`
	Port        int    `gorm:"not null"`
	Weight      int    `gorm:"not null"`
	MaxFails    int    `gorm:"not null"`
	FailTimeout int    `gorm:"not null"`
	Backup      bool
	Down        bool
	Sort        int `gorm:"not null"`
}

// Site 是一条七层站点：监听一个端口，按域名反代到一个 HTTP 上游。当前每条站点只有 location /。
type Site struct {
	ID            string `gorm:"primaryKey;size:36"`
	ClusterID     string `gorm:"size:36;not null;uniqueIndex:idx_site_name;index"`
	Name          string `gorm:"size:32;not null;uniqueIndex:idx_site_name"`
	ListenPort    int    `gorm:"not null"`
	ServerName    string `gorm:"size:512;not null"`
	TLS           bool
	CertificateID string `gorm:"size:36"`
	UpstreamID    string `gorm:"size:36;not null"`
	WebSocket     bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Stream 是一条四层代理，TCP 或 UDP，把监听端口转到一个 stream 上游。
type Stream struct {
	ID         string `gorm:"primaryKey;size:36"`
	ClusterID  string `gorm:"size:36;not null;uniqueIndex:idx_stream_name;index"`
	Name       string `gorm:"size:32;not null;uniqueIndex:idx_stream_name"`
	Protocol   string `gorm:"size:8;not null"`
	ListenPort int    `gorm:"not null"`
	UpstreamID string `gorm:"size:36;not null"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Certificate 保存上传的证书和私钥。私钥加密后存放，预览和接口都不会再把它吐出来。
type Certificate struct {
	ID          string `gorm:"primaryKey;size:36"`
	ClusterID   string `gorm:"size:36;not null;uniqueIndex:idx_cert_name;index"`
	Name        string `gorm:"size:32;not null;uniqueIndex:idx_cert_name"`
	Subject     string `gorm:"size:256"`
	Fingerprint string `gorm:"size:128"`
	DNSNames    string `gorm:"size:1024"`
	NotAfter    time.Time
	CertEnc     string `gorm:"type:text" json:"-"`
	KeyEnc      string `gorm:"type:text" json:"-"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Revision 是一次发布或回滚时渲染出来的文件快照。ManifestEnc 里是加密后的文件内容，回滚时解密再写回节点。
type Revision struct {
	ID          string `gorm:"primaryKey;size:36"`
	ClusterID   string `gorm:"size:36;not null;index"`
	Hash        string `gorm:"size:64;not null"`
	Status      string `gorm:"size:16;not null"`
	Note        string `gorm:"size:256"`
	CreatedBy   string `gorm:"size:64"`
	ManifestEnc string `gorm:"type:text" json:"-"`
	CreatedAt   time.Time
}

// DeployJob 是一次安装、发布或回滚的执行记录。接口先返回它，真正的 SSH 在后台做，Log 里追加过程。
type DeployJob struct {
	ID         string `gorm:"primaryKey;size:36"`
	ClusterID  string `gorm:"size:36;not null;index"`
	RevisionID string `gorm:"size:36"`
	Action     string `gorm:"size:16;not null"`
	Status     string `gorm:"size:16;not null;index"`
	Log        string `gorm:"type:text"`
	CreatedBy  string `gorm:"size:64"`
	CreatedAt  time.Time
	FinishedAt *time.Time
}
