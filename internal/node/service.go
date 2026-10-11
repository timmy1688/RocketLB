// Package node 管理节点清单。节点只是一台可以 SSH 上去的机器，加入集群之后才会参与 VIP 和发布。
package node

import (
	"context"
	"rocketlb/internal/httpx"
	"strings"
	"time"

	"gorm.io/gorm"

	"rocketlb/internal/model"
	"rocketlb/internal/secret"
	"rocketlb/internal/sshclient"
	"rocketlb/internal/store"
	"rocketlb/internal/validate"
)

// probeScript 是探测节点时执行的命令：内核版本，以及 /etc/os-release 里的系统名称。
const probeScript = `uname -srm
if [ -r /etc/os-release ]; then
  . /etc/os-release
  printf '%s\n' "${PRETTY_NAME:-unknown}"
fi
`

// View 是返回给界面的节点。密码和私钥不会出现在这里，只给主机密钥的指纹。
type View struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Address     string     `json:"address"`
	SSHPort     int        `json:"sshPort"`
	SSHUser     string     `json:"sshUser"`
	AuthType    string     `json:"authType"`
	Fingerprint string     `json:"fingerprint"`
	OS          string     `json:"os"`
	Status      string     `json:"status"`
	LastError   string     `json:"lastError"`
	LastProbeAt *time.Time `json:"lastProbeAt"`
	CreatedAt   time.Time  `json:"createdAt"`
}

// Write 是创建或修改节点时前端提交的内容。Secret 是明文密码或私钥，入库前会加密。
type Write struct {
	Name     string `json:"name"`
	Address  string `json:"address"`
	SSHPort  int    `json:"sshPort"`
	SSHUser  string `json:"sshUser"`
	AuthType string `json:"authType"`
	Secret   string `json:"secret"`
}

// Service 负责节点的增删改查和探测。
type Service struct {
	db      *gorm.DB
	box     *secret.Box
	ssh     sshclient.Runner
	timeout time.Duration
}

// NewService 在启动时创建。发布和安装也会借用它的 Spec，把数据库里的节点变成 SSH 连接参数。
func NewService(db *gorm.DB, box *secret.Box, ssh sshclient.Runner, timeout time.Duration) *Service {
	return &Service{db: db, box: box, ssh: ssh, timeout: timeout}
}

// Spec 解密节点上保存的 SSH 密码或私钥，组装成一次连接所需的参数。
func Spec(box *secret.Box, n model.Node, timeout time.Duration) (sshclient.Spec, error) {
	secretText, err := box.Decrypt(n.SecretEnc)
	if err != nil {
		return sshclient.Spec{}, err
	}
	spec := sshclient.Spec{
		Address:      n.Address,
		Port:         n.SSHPort,
		User:         n.SSHUser,
		KnownHostKey: n.HostKey,
		Timeout:      timeout,
	}
	switch n.AuthType {
	case model.AuthKey:
		spec.PrivateKey = []byte(secretText)
	case model.AuthPassword:
		spec.Password = secretText
	default:
		return sshclient.Spec{}, httpx.Invalid("未知的认证方式")
	}
	return spec, nil
}

// List 返回全部节点，按名字排序。
func (s *Service) List(ctx context.Context) ([]View, error) {
	var rows []model.Node
	if err := s.db.WithContext(ctx).Order("name asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]View, 0, len(rows))
	for _, row := range rows {
		out = append(out, toView(row))
	}
	return out, nil
}

// Get 按编号取一台节点。
func (s *Service) Get(ctx context.Context, id string) (View, error) {
	row, err := s.load(ctx, id)
	if err != nil {
		return View{}, err
	}
	return toView(row), nil
}

// Create 校验后加密凭据再入库。此时还不会 SSH，探测是另一步。
func (s *Service) Create(ctx context.Context, in Write) (View, error) {
	if err := checkWrite(in, true); err != nil {
		return View{}, err
	}
	enc, err := s.box.Encrypt(in.Secret)
	if err != nil {
		return View{}, err
	}
	row := model.Node{
		ID:        model.NewID(),
		Name:      in.Name,
		Address:   in.Address,
		SSHPort:   portOr(in.SSHPort),
		SSHUser:   in.SSHUser,
		AuthType:  in.AuthType,
		SecretEnc: enc,
		Status:    model.NodeUnknown,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if store.Duplicate(err) {
			return View{}, httpx.Conflict("节点名称已存在")
		}
		return View{}, err
	}
	return toView(row), nil
}

// Update 修改节点。地址或端口变了就忘掉旧的主机密钥，下次探测重新信任。Secret 留空表示不改密码。
func (s *Service) Update(ctx context.Context, id string, in Write) (View, error) {
	row, err := s.load(ctx, id)
	if err != nil {
		return View{}, err
	}
	if err := checkWrite(in, false); err != nil {
		return View{}, err
	}
	if in.AuthType != row.AuthType && in.Secret == "" {
		return View{}, httpx.Invalid("切换认证方式时需要填写密钥")
	}
	updates := map[string]any{
		"name":      in.Name,
		"address":   in.Address,
		"ssh_port":  portOr(in.SSHPort),
		"ssh_user":  in.SSHUser,
		"auth_type": in.AuthType,
	}
	if in.Secret != "" {
		enc, err := s.box.Encrypt(in.Secret)
		if err != nil {
			return View{}, err
		}
		updates["secret_enc"] = enc
	}
	if in.Address != row.Address || in.SSHPort != row.SSHPort {
		updates["host_key"] = ""
		updates["status"] = model.NodeUnknown
	}
	if err := s.db.WithContext(ctx).Model(&row).Updates(updates).Error; err != nil {
		if store.Duplicate(err) {
			return View{}, httpx.Conflict("节点名称已存在")
		}
		return View{}, err
	}
	return s.Get(ctx, id)
}

// Delete 删除节点。节点还在某个集群里时拒绝，避免集群指向一台已经不存在的机器。
func (s *Service) Delete(ctx context.Context, id string) error {
	if _, err := s.load(ctx, id); err != nil {
		return err
	}
	var members int64
	if err := s.db.WithContext(ctx).Model(&model.ClusterMember{}).Where("node_id = ?", id).Count(&members).Error; err != nil {
		return err
	}
	if members > 0 {
		return httpx.Conflict("节点仍在集群中，先从集群移除")
	}
	return s.db.WithContext(ctx).Delete(&model.Node{}, "id = ?", id).Error
}

// Probe 真正连一次 SSH，记下在线状态、系统描述，并在第一次成功时保存主机公钥。
func (s *Service) Probe(ctx context.Context, id string) (View, error) {
	row, err := s.load(ctx, id)
	if err != nil {
		return View{}, err
	}
	spec, err := Spec(s.box, row, s.timeout)
	if err != nil {
		return View{}, err
	}
	res, runErr := s.ssh.Run(ctx, spec, probeScript, false)
	now := time.Now()
	updates := map[string]any{"last_probe_at": now}
	if res.HostKey != "" && row.HostKey == "" {
		updates["host_key"] = res.HostKey
	}
	if runErr != nil {
		updates["status"] = model.NodeOffline
		updates["last_error"] = clip(runErr.Error(), 500)
	} else {
		updates["status"] = model.NodeOnline
		updates["last_error"] = ""
		updates["os"] = parseOS(res.Stdout)
	}
	if err := s.db.WithContext(ctx).Model(&row).Updates(updates).Error; err != nil {
		return View{}, err
	}
	return s.Get(ctx, id)
}

// load 从数据库读一行，没有就返回 404。
func (s *Service) load(ctx context.Context, id string) (model.Node, error) {
	var row model.Node
	err := s.db.WithContext(ctx).First(&row, "id = ?", id).Error
	if err != nil {
		return model.Node{}, store.NotFound(err, "节点不存在")
	}
	return row, nil
}

// checkWrite 检查名字、IPv4、端口和认证方式。新建时必须带密码或私钥。
func checkWrite(in Write, creating bool) error {
	if err := validate.Name(in.Name); err != nil {
		return httpx.Invalid(err.Error())
	}
	if err := validate.IPv4(in.Address); err != nil {
		return httpx.Invalid("节点地址需为 IPv4")
	}
	if in.SSHPort != 0 {
		if err := validate.Port(in.SSHPort); err != nil {
			return httpx.Invalid(err.Error())
		}
	}
	if err := validate.SSHUser(in.SSHUser); err != nil {
		return httpx.Invalid(err.Error())
	}
	if in.AuthType != model.AuthPassword && in.AuthType != model.AuthKey {
		return httpx.Invalid("认证方式只能是 password 或 key")
	}
	if creating && strings.TrimSpace(in.Secret) == "" {
		return httpx.Invalid("需要填写 SSH 密码或私钥")
	}
	return nil
}

// toView 去掉密文，只把指纹交给界面。
func toView(row model.Node) View {
	return View{
		ID:          row.ID,
		Name:        row.Name,
		Address:     row.Address,
		SSHPort:     row.SSHPort,
		SSHUser:     row.SSHUser,
		AuthType:    row.AuthType,
		Fingerprint: sshclient.Fingerprint(row.HostKey),
		OS:          row.OS,
		Status:      row.Status,
		LastError:   row.LastError,
		LastProbeAt: row.LastProbeAt,
		CreatedAt:   row.CreatedAt,
	}
}

// portOr 把没填的 SSH 端口当成 22。
func portOr(port int) int {
	if port == 0 {
		return 22
	}
	return port
}

// parseOS 从探测脚本的输出里取出系统描述。有两行时用第二行的 PRETTY_NAME。
func parseOS(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return ""
	}
	if len(lines) >= 2 {
		return strings.TrimSpace(lines[len(lines)-1])
	}
	return strings.TrimSpace(lines[0])
}

// clip 截断过长的错误信息，避免把整段 SSH 输出写进数据库。
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
