// Package proxy 保存四层代理、七层站点、上游和证书。改这些表不会立刻影响线上，要点集群的发布才会渲染并下发。
package proxy

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"rocketlb/internal/httpx"
	"strings"
	"time"

	"gorm.io/gorm"

	"rocketlb/internal/model"
	"rocketlb/internal/secret"
	"rocketlb/internal/store"
	"rocketlb/internal/validate"
)

// Server 是界面上的一台后端。
type Server struct {
	Address     string `json:"address"`
	Port        int    `json:"port"`
	Weight      int    `json:"weight"`
	MaxFails    int    `json:"maxFails"`
	FailTimeout int    `json:"failTimeout"`
	Backup      bool   `json:"backup"`
	Down        bool   `json:"down"`
}

// Upstream 是一组后端，Layer 为 http 或 stream。
type Upstream struct {
	ID        string    `json:"id"`
	ClusterID string    `json:"clusterId"`
	Name      string    `json:"name"`
	Layer     string    `json:"layer"`
	Method    string    `json:"method"`
	Servers   []Server  `json:"servers"`
	CreatedAt time.Time `json:"createdAt"`
}

// UpstreamWrite 是新建或修改上游的请求体。
type UpstreamWrite struct {
	Name    string   `json:"name"`
	Layer   string   `json:"layer"`
	Method  string   `json:"method"`
	Servers []Server `json:"servers"`
}

// Site 是一条七层站点。
type Site struct {
	ID            string    `json:"id"`
	ClusterID     string    `json:"clusterId"`
	Name          string    `json:"name"`
	ListenPort    int       `json:"listenPort"`
	ServerName    string    `json:"serverName"`
	TLS           bool      `json:"tls"`
	CertificateID string    `json:"certificateId"`
	UpstreamID    string    `json:"upstreamId"`
	WebSocket     bool      `json:"webSocket"`
	CreatedAt     time.Time `json:"createdAt"`
}

// SiteWrite 是新建或修改站点的请求体。
type SiteWrite struct {
	Name          string `json:"name"`
	ListenPort    int    `json:"listenPort"`
	ServerName    string `json:"serverName"`
	TLS           bool   `json:"tls"`
	CertificateID string `json:"certificateId"`
	UpstreamID    string `json:"upstreamId"`
	WebSocket     bool   `json:"webSocket"`
}

// Stream 是一条四层代理。
type Stream struct {
	ID         string    `json:"id"`
	ClusterID  string    `json:"clusterId"`
	Name       string    `json:"name"`
	Protocol   string    `json:"protocol"`
	ListenPort int       `json:"listenPort"`
	UpstreamID string    `json:"upstreamId"`
	CreatedAt  time.Time `json:"createdAt"`
}

// StreamWrite 是新建或修改四层代理的请求体。
type StreamWrite struct {
	Name       string `json:"name"`
	Protocol   string `json:"protocol"`
	ListenPort int    `json:"listenPort"`
	UpstreamID string `json:"upstreamId"`
}

// Certificate 是证书摘要。私钥不会返回，只有指纹。
type Certificate struct {
	ID          string    `json:"id"`
	ClusterID   string    `json:"clusterId"`
	Name        string    `json:"name"`
	Subject     string    `json:"subject"`
	Fingerprint string    `json:"fingerprint"`
	DNSNames    string    `json:"dnsNames"`
	NotAfter    time.Time `json:"notAfter"`
	CreatedAt   time.Time `json:"createdAt"`
}

// CertificateWrite 是上传证书时的请求体，里面有证书和私钥的 PEM。
type CertificateWrite struct {
	Name        string `json:"name"`
	Certificate string `json:"certificate"`
	PrivateKey  string `json:"privateKey"`
}

// Service 保存四层、七层、上游和证书。这些都只是期望状态，要点发布才会写到节点。
type Service struct {
	db  *gorm.DB
	box *secret.Box
}

// NewService 在启动时创建。box 用来加密证书私钥。
func NewService(db *gorm.DB, box *secret.Box) *Service {
	return &Service{db: db, box: box}
}

// ListUpstreams 列出一个集群的全部上游和后端。
func (s *Service) ListUpstreams(ctx context.Context, clusterID string) ([]Upstream, error) {
	if err := s.ensureCluster(ctx, clusterID); err != nil {
		return nil, err
	}
	var rows []model.Upstream
	err := s.db.WithContext(ctx).Preload("Servers", func(db *gorm.DB) *gorm.DB {
		return db.Order("sort asc")
	}).Where("cluster_id = ?", clusterID).Order("name asc").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]Upstream, 0, len(rows))
	for _, row := range rows {
		out = append(out, upstreamView(row))
	}
	return out, nil
}

// CreateUpstream 校验算法和后端后写入上游。
func (s *Service) CreateUpstream(ctx context.Context, clusterID string, in UpstreamWrite) (Upstream, error) {
	if err := s.ensureCluster(ctx, clusterID); err != nil {
		return Upstream{}, err
	}
	row, servers, err := buildUpstream(clusterID, in)
	if err != nil {
		return Upstream{}, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		for i := range servers {
			servers[i].UpstreamID = row.ID
		}
		if len(servers) == 0 {
			return nil
		}
		return tx.Create(&servers).Error
	})
	if store.Duplicate(err) {
		return Upstream{}, httpx.Conflict("上游名称已存在")
	}
	if err != nil {
		return Upstream{}, err
	}
	row.Servers = servers
	return upstreamView(row), nil
}

// UpdateUpstream 替换上游的算法和后端列表。
func (s *Service) UpdateUpstream(ctx context.Context, id string, in UpstreamWrite) (Upstream, error) {
	current, err := s.loadUpstream(ctx, id)
	if err != nil {
		return Upstream{}, err
	}
	row, servers, err := buildUpstream(current.ClusterID, in)
	if err != nil {
		return Upstream{}, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Model(&model.Upstream{}).Where("id = ?", id).Updates(map[string]any{
			"name":   row.Name,
			"layer":  row.Layer,
			"method": row.Method,
		}).Error
		if err != nil {
			return err
		}
		if err := tx.Where("upstream_id = ?", id).Delete(&model.UpstreamServer{}).Error; err != nil {
			return err
		}
		for i := range servers {
			servers[i].UpstreamID = id
		}
		if len(servers) == 0 {
			return nil
		}
		return tx.Create(&servers).Error
	})
	if store.Duplicate(err) {
		return Upstream{}, httpx.Conflict("上游名称已存在")
	}
	if err != nil {
		return Upstream{}, err
	}
	return s.upstreamByID(ctx, id)
}

// DeleteUpstream 删除上游。仍被站点或四层代理引用时拒绝。
func (s *Service) DeleteUpstream(ctx context.Context, id string) error {
	current, err := s.loadUpstream(ctx, id)
	if err != nil {
		return err
	}
	var sites int64
	if err := s.db.WithContext(ctx).Model(&model.Site{}).Where("upstream_id = ?", id).Count(&sites).Error; err != nil {
		return err
	}
	var streams int64
	if err := s.db.WithContext(ctx).Model(&model.Stream{}).Where("upstream_id = ?", id).Count(&streams).Error; err != nil {
		return err
	}
	if sites+streams > 0 {
		return httpx.Conflict("上游仍被站点或四层代理使用")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("upstream_id = ?", current.ID).Delete(&model.UpstreamServer{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.Upstream{}, "id = ?", current.ID).Error
	})
}

// ListSites 列出七层站点。
func (s *Service) ListSites(ctx context.Context, clusterID string) ([]Site, error) {
	if err := s.ensureCluster(ctx, clusterID); err != nil {
		return nil, err
	}
	var rows []model.Site
	if err := s.db.WithContext(ctx).Where("cluster_id = ?", clusterID).Order("name asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Site, 0, len(rows))
	for _, row := range rows {
		out = append(out, siteView(row))
	}
	return out, nil
}

// CreateSite 新建站点，并确认它引用的是本集群的七层上游。
func (s *Service) CreateSite(ctx context.Context, clusterID string, in SiteWrite) (Site, error) {
	if err := s.ensureCluster(ctx, clusterID); err != nil {
		return Site{}, err
	}
	if err := s.checkSite(ctx, clusterID, "", in); err != nil {
		return Site{}, err
	}
	row := siteModel(clusterID, in)
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if store.Duplicate(err) {
			return Site{}, httpx.Conflict("站点名称已存在")
		}
		return Site{}, err
	}
	return siteView(row), nil
}

// UpdateSite 修改站点。监听端口不能和别的站点或四层代理冲突。
func (s *Service) UpdateSite(ctx context.Context, id string, in SiteWrite) (Site, error) {
	current, err := s.loadSite(ctx, id)
	if err != nil {
		return Site{}, err
	}
	if err := s.checkSite(ctx, current.ClusterID, id, in); err != nil {
		return Site{}, err
	}
	row := siteModel(current.ClusterID, in)
	err = s.db.WithContext(ctx).Model(&model.Site{}).Where("id = ?", id).Updates(map[string]any{
		"name":           row.Name,
		"listen_port":    row.ListenPort,
		"server_name":    row.ServerName,
		"tls":            row.TLS,
		"certificate_id": row.CertificateID,
		"upstream_id":    row.UpstreamID,
		"web_socket":     row.WebSocket,
	}).Error
	if store.Duplicate(err) {
		return Site{}, httpx.Conflict("站点名称已存在")
	}
	if err != nil {
		return Site{}, err
	}
	return s.siteByID(ctx, id)
}

// DeleteSite 删除一条七层站点。
func (s *Service) DeleteSite(ctx context.Context, id string) error {
	if _, err := s.loadSite(ctx, id); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Delete(&model.Site{}, "id = ?", id).Error
}

// ListStreams 列出四层代理。
func (s *Service) ListStreams(ctx context.Context, clusterID string) ([]Stream, error) {
	if err := s.ensureCluster(ctx, clusterID); err != nil {
		return nil, err
	}
	var rows []model.Stream
	if err := s.db.WithContext(ctx).Where("cluster_id = ?", clusterID).Order("name asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Stream, 0, len(rows))
	for _, row := range rows {
		out = append(out, streamView(row))
	}
	return out, nil
}

// CreateStream 新建四层代理，上游必须是 stream 层。
func (s *Service) CreateStream(ctx context.Context, clusterID string, in StreamWrite) (Stream, error) {
	if err := s.ensureCluster(ctx, clusterID); err != nil {
		return Stream{}, err
	}
	if err := s.checkStream(ctx, clusterID, "", in); err != nil {
		return Stream{}, err
	}
	row := streamModel(clusterID, in)
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if store.Duplicate(err) {
			return Stream{}, httpx.Conflict("四层代理名称已存在")
		}
		return Stream{}, err
	}
	return streamView(row), nil
}

// UpdateStream 修改四层代理。
func (s *Service) UpdateStream(ctx context.Context, id string, in StreamWrite) (Stream, error) {
	current, err := s.loadStream(ctx, id)
	if err != nil {
		return Stream{}, err
	}
	if err := s.checkStream(ctx, current.ClusterID, id, in); err != nil {
		return Stream{}, err
	}
	row := streamModel(current.ClusterID, in)
	err = s.db.WithContext(ctx).Model(&model.Stream{}).Where("id = ?", id).Updates(map[string]any{
		"name":        row.Name,
		"protocol":    row.Protocol,
		"listen_port": row.ListenPort,
		"upstream_id": row.UpstreamID,
	}).Error
	if store.Duplicate(err) {
		return Stream{}, httpx.Conflict("四层代理名称已存在")
	}
	if err != nil {
		return Stream{}, err
	}
	return s.streamByID(ctx, id)
}

// DeleteStream 删除一条四层代理。
func (s *Service) DeleteStream(ctx context.Context, id string) error {
	if _, err := s.loadStream(ctx, id); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Delete(&model.Stream{}, "id = ?", id).Error
}

// ListCertificates 列出证书摘要，不返回私钥。
func (s *Service) ListCertificates(ctx context.Context, clusterID string) ([]Certificate, error) {
	if err := s.ensureCluster(ctx, clusterID); err != nil {
		return nil, err
	}
	var rows []model.Certificate
	if err := s.db.WithContext(ctx).Where("cluster_id = ?", clusterID).Order("name asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Certificate, 0, len(rows))
	for _, row := range rows {
		out = append(out, certView(row))
	}
	return out, nil
}

// CreateCertificate 解析 PEM，确认证书和私钥成对，然后只把私钥加密入库。
func (s *Service) CreateCertificate(ctx context.Context, clusterID string, in CertificateWrite) (Certificate, error) {
	if err := s.ensureCluster(ctx, clusterID); err != nil {
		return Certificate{}, err
	}
	if err := validate.Name(in.Name); err != nil {
		return Certificate{}, httpx.Invalid(err.Error())
	}
	leaf, err := parseCert(in.Certificate, in.PrivateKey)
	if err != nil {
		return Certificate{}, err
	}
	certEnc, err := s.box.Encrypt(in.Certificate)
	if err != nil {
		return Certificate{}, err
	}
	keyEnc, err := s.box.Encrypt(in.PrivateKey)
	if err != nil {
		return Certificate{}, err
	}
	sum := sha256.Sum256(leaf.Raw)
	row := model.Certificate{
		ID:          model.NewID(),
		ClusterID:   clusterID,
		Name:        in.Name,
		Subject:     leaf.Subject.CommonName,
		Fingerprint: hex.EncodeToString(sum[:]),
		DNSNames:    strings.Join(certNames(leaf), ", "),
		NotAfter:    leaf.NotAfter,
		CertEnc:     certEnc,
		KeyEnc:      keyEnc,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if store.Duplicate(err) {
			return Certificate{}, httpx.Conflict("证书名称已存在")
		}
		return Certificate{}, err
	}
	return certView(row), nil
}

// DeleteCertificate 删除证书。仍被站点使用时拒绝。
func (s *Service) DeleteCertificate(ctx context.Context, id string) error {
	var row model.Certificate
	if err := s.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		return store.NotFound(err, "证书不存在")
	}
	var sites int64
	if err := s.db.WithContext(ctx).Model(&model.Site{}).Where("certificate_id = ?", id).Count(&sites).Error; err != nil {
		return err
	}
	if sites > 0 {
		return httpx.Conflict("证书仍被站点使用")
	}
	return s.db.WithContext(ctx).Delete(&model.Certificate{}, "id = ?", id).Error
}

// ensureCluster 确认这些代理配置属于一个还存在的集群。
func (s *Service) ensureCluster(ctx context.Context, id string) error {
	var count int64
	if err := s.db.WithContext(ctx).Model(&model.Cluster{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return httpx.NotFound("集群不存在")
	}
	return nil
}

// checkSite 检查域名、端口、上游层级，以及开启 TLS 时证书是否存在。
func (s *Service) checkSite(ctx context.Context, clusterID, self string, in SiteWrite) error {
	if err := validate.Name(in.Name); err != nil {
		return httpx.Invalid(err.Error())
	}
	if err := validate.Port(in.ListenPort); err != nil {
		return httpx.Invalid(err.Error())
	}
	if err := validate.ServerName(in.ServerName); err != nil {
		return httpx.Invalid(err.Error())
	}
	if _, err := s.loadLayerUpstream(ctx, clusterID, in.UpstreamID, model.LayerHTTP); err != nil {
		return err
	}
	if in.TLS {
		if in.CertificateID == "" {
			return httpx.Invalid("启用 TLS 时需要选择证书")
		}
		var cert model.Certificate
		err := s.db.WithContext(ctx).Where("id = ? AND cluster_id = ?", in.CertificateID, clusterID).First(&cert).Error
		if err != nil {
			return store.NotFound(err, "证书不存在")
		}
	}
	return s.siteListenFree(ctx, clusterID, self, in)
}

// checkStream 检查协议、端口和四层上游。
func (s *Service) checkStream(ctx context.Context, clusterID, self string, in StreamWrite) error {
	if err := validate.Name(in.Name); err != nil {
		return httpx.Invalid(err.Error())
	}
	if in.Protocol == "" {
		in.Protocol = "tcp"
	}
	if in.Protocol != "tcp" && in.Protocol != "udp" {
		return httpx.Invalid("协议只能是 tcp 或 udp")
	}
	if err := validate.Port(in.ListenPort); err != nil {
		return httpx.Invalid(err.Error())
	}
	if _, err := s.loadLayerUpstream(ctx, clusterID, in.UpstreamID, model.LayerStream); err != nil {
		return err
	}
	return s.listenTaken(ctx, clusterID, "", self, in.ListenPort, in.Protocol)
}

// siteListenFree 允许同一个七层端口挂多个域名。域名重叠，或 HTTP 和 HTTPS 混在同一端口，才拒绝。
// 四层没有域名，仍然独占整个端口。
func (s *Service) siteListenFree(ctx context.Context, clusterID, self string, in SiteWrite) error {
	var streams int64
	err := s.db.WithContext(ctx).Model(&model.Stream{}).
		Where("cluster_id = ? AND listen_port = ? AND protocol = ?", clusterID, in.ListenPort, "tcp").
		Count(&streams).Error
	if err != nil {
		return err
	}
	if streams > 0 {
		return httpx.Conflict(validate.ListenConflict(in.ListenPort, "tcp"))
	}
	q := s.db.WithContext(ctx).Model(&model.Site{}).Where("cluster_id = ? AND listen_port = ?", clusterID, in.ListenPort)
	if self != "" {
		q = q.Where("id <> ?", self)
	}
	var rows []model.Site
	if err := q.Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if row.TLS != in.TLS {
			return httpx.Conflictf("端口 %d 不能同时做 HTTP 和 HTTPS", in.ListenPort)
		}
		if name, ok := validate.OverlappingName(row.ServerName, in.ServerName); ok {
			return httpx.Conflictf("端口 %d 上的域名 %s 已被占用", in.ListenPort, name)
		}
	}
	return nil
}

// listenTaken 查看同一集群里是否已有别的站点或四层代理占用这个端口。
func (s *Service) listenTaken(ctx context.Context, clusterID, exceptSite, exceptStream string, port int, protocol string) error {
	if protocol == "tcp" {
		q := s.db.WithContext(ctx).Model(&model.Site{}).Where("cluster_id = ? AND listen_port = ?", clusterID, port)
		if exceptSite != "" {
			q = q.Where("id <> ?", exceptSite)
		}
		var sites int64
		if err := q.Count(&sites).Error; err != nil {
			return err
		}
		if sites > 0 {
			return httpx.Conflict(validate.ListenConflict(port, "tcp"))
		}
	}
	q := s.db.WithContext(ctx).Model(&model.Stream{}).Where("cluster_id = ? AND listen_port = ? AND protocol = ?", clusterID, port, protocol)
	if exceptStream != "" {
		q = q.Where("id <> ?", exceptStream)
	}
	var streams int64
	if err := q.Count(&streams).Error; err != nil {
		return err
	}
	if streams > 0 {
		return httpx.Conflict(validate.ListenConflict(port, protocol))
	}
	return nil
}

// loadLayerUpstream 读取指定层级的上游，防止七层站点误用四层上游。
func (s *Service) loadLayerUpstream(ctx context.Context, clusterID, id, layer string) (model.Upstream, error) {
	var row model.Upstream
	err := s.db.WithContext(ctx).Where("id = ? AND cluster_id = ?", id, clusterID).First(&row).Error
	if err != nil {
		label := "七层上游"
		if layer == model.LayerStream {
			label = "四层上游"
		}
		return model.Upstream{}, store.NotFound(err, label+"不存在")
	}
	if row.Layer != layer {
		return model.Upstream{}, httpx.Invalid("上游类型不匹配")
	}
	return row, nil
}

// loadUpstream 按编号读上游。
func (s *Service) loadUpstream(ctx context.Context, id string) (model.Upstream, error) {
	var row model.Upstream
	if err := s.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		return model.Upstream{}, store.NotFound(err, "上游不存在")
	}
	return row, nil
}

// loadSite 按编号读站点。
func (s *Service) loadSite(ctx context.Context, id string) (model.Site, error) {
	var row model.Site
	if err := s.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		return model.Site{}, store.NotFound(err, "站点不存在")
	}
	return row, nil
}

// loadStream 按编号读四层代理。
func (s *Service) loadStream(ctx context.Context, id string) (model.Stream, error) {
	var row model.Stream
	if err := s.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		return model.Stream{}, store.NotFound(err, "四层代理不存在")
	}
	return row, nil
}

// upstreamByID 读出上游并带上后端，作为接口返回值。
func (s *Service) upstreamByID(ctx context.Context, id string) (Upstream, error) {
	var row model.Upstream
	err := s.db.WithContext(ctx).Preload("Servers", func(db *gorm.DB) *gorm.DB {
		return db.Order("sort asc")
	}).First(&row, "id = ?", id).Error
	if err != nil {
		return Upstream{}, err
	}
	return upstreamView(row), nil
}

// siteByID 读出一条站点并转成接口返回值。
func (s *Service) siteByID(ctx context.Context, id string) (Site, error) {
	row, err := s.loadSite(ctx, id)
	if err != nil {
		return Site{}, err
	}
	return siteView(row), nil
}

// streamByID 读出一条四层代理并转成接口返回值。
func (s *Service) streamByID(ctx context.Context, id string) (Stream, error) {
	row, err := s.loadStream(ctx, id)
	if err != nil {
		return Stream{}, err
	}
	return streamView(row), nil
}

// buildUpstream 把请求体转成数据库行，并检查每一台后端。
func buildUpstream(clusterID string, in UpstreamWrite) (model.Upstream, []model.UpstreamServer, error) {
	if err := validate.Name(in.Name); err != nil {
		return model.Upstream{}, nil, httpx.Invalid(err.Error())
	}
	if in.Layer != model.LayerHTTP && in.Layer != model.LayerStream {
		return model.Upstream{}, nil, httpx.Invalid("上游层级只能是 http 或 stream")
	}
	if in.Method == "" {
		in.Method = "round_robin"
	}
	if err := validMethod(in.Layer, in.Method); err != nil {
		return model.Upstream{}, nil, err
	}
	if len(in.Servers) == 0 {
		return model.Upstream{}, nil, httpx.Invalid("至少需要一个后端")
	}
	row := model.Upstream{
		ID:        model.NewID(),
		ClusterID: clusterID,
		Name:      in.Name,
		Layer:     in.Layer,
		Method:    in.Method,
	}
	servers := make([]model.UpstreamServer, 0, len(in.Servers))
	for i, server := range in.Servers {
		normalized, err := normalizeServer(server, i)
		if err != nil {
			return model.Upstream{}, nil, err
		}
		servers = append(servers, normalized)
	}
	return row, servers, nil
}

// validMethod 限制算法。四层不能用 ip_hash，开源 nginx 的 stream 没有这条指令。
func validMethod(layer, method string) error {
	switch method {
	case "round_robin", "least_conn", "hash":
		return nil
	case "ip_hash":
		if layer != model.LayerHTTP {
			return httpx.Invalid("四层上游不能使用 ip_hash")
		}
		return nil
	default:
		return httpx.Invalid("不支持的负载算法")
	}
}

// normalizeServer 检查后端地址和端口，并补上默认权重、失败次数和超时。
func normalizeServer(in Server, sort int) (model.UpstreamServer, error) {
	if err := validate.Host(in.Address); err != nil {
		return model.UpstreamServer{}, httpx.Invalid(err.Error())
	}
	if err := validate.Port(in.Port); err != nil {
		return model.UpstreamServer{}, httpx.Invalid(err.Error())
	}
	if in.Weight == 0 {
		in.Weight = 1
	}
	if in.Weight < 1 || in.Weight > 100 {
		return model.UpstreamServer{}, httpx.Invalid("权重需在 1 到 100 之间")
	}
	if in.MaxFails == 0 {
		in.MaxFails = 3
	}
	if in.FailTimeout == 0 {
		in.FailTimeout = 10
	}
	if in.MaxFails < 1 || in.MaxFails > 100 || in.FailTimeout < 1 || in.FailTimeout > 3600 {
		return model.UpstreamServer{}, httpx.Invalid("健康检查参数超出范围")
	}
	return model.UpstreamServer{
		ID:          model.NewID(),
		Address:     in.Address,
		Port:        in.Port,
		Weight:      in.Weight,
		MaxFails:    in.MaxFails,
		FailTimeout: in.FailTimeout,
		Backup:      in.Backup,
		Down:        in.Down,
		Sort:        sort,
	}, nil
}

// siteModel 把站点请求体转成数据库行。
func siteModel(clusterID string, in SiteWrite) model.Site {
	certID := ""
	if in.TLS {
		certID = in.CertificateID
	}
	return model.Site{
		ID:            model.NewID(),
		ClusterID:     clusterID,
		Name:          in.Name,
		ListenPort:    in.ListenPort,
		ServerName:    in.ServerName,
		TLS:           in.TLS,
		CertificateID: certID,
		UpstreamID:    in.UpstreamID,
		WebSocket:     in.WebSocket,
	}
}

// streamModel 把四层代理请求体转成数据库行。
func streamModel(clusterID string, in StreamWrite) model.Stream {
	protocol := in.Protocol
	if protocol == "" {
		protocol = "tcp"
	}
	return model.Stream{
		ID:         model.NewID(),
		ClusterID:  clusterID,
		Name:       in.Name,
		Protocol:   protocol,
		ListenPort: in.ListenPort,
		UpstreamID: in.UpstreamID,
	}
}

// upstreamView 把上游行转成接口结构，后端由调用方另填。
func upstreamView(row model.Upstream) Upstream {
	servers := make([]Server, 0, len(row.Servers))
	for _, server := range row.Servers {
		servers = append(servers, Server{
			Address: server.Address, Port: server.Port, Weight: server.Weight,
			MaxFails: server.MaxFails, FailTimeout: server.FailTimeout,
			Backup: server.Backup, Down: server.Down,
		})
	}
	return Upstream{
		ID: row.ID, ClusterID: row.ClusterID, Name: row.Name, Layer: row.Layer,
		Method: row.Method, Servers: servers, CreatedAt: row.CreatedAt,
	}
}

// siteView 把站点行转成接口结构。
func siteView(row model.Site) Site {
	return Site{
		ID: row.ID, ClusterID: row.ClusterID, Name: row.Name, ListenPort: row.ListenPort,
		ServerName: row.ServerName, TLS: row.TLS, CertificateID: row.CertificateID,
		UpstreamID: row.UpstreamID, WebSocket: row.WebSocket, CreatedAt: row.CreatedAt,
	}
}

// streamView 把四层代理行转成接口结构。
func streamView(row model.Stream) Stream {
	return Stream{
		ID: row.ID, ClusterID: row.ClusterID, Name: row.Name, Protocol: row.Protocol,
		ListenPort: row.ListenPort, UpstreamID: row.UpstreamID, CreatedAt: row.CreatedAt,
	}
}

// certView 把证书行转成摘要，不带私钥。
func certView(row model.Certificate) Certificate {
	return Certificate{
		ID: row.ID, ClusterID: row.ClusterID, Name: row.Name, Subject: row.Subject,
		Fingerprint: row.Fingerprint, DNSNames: row.DNSNames, NotAfter: row.NotAfter,
		CreatedAt: row.CreatedAt,
	}
}

// parseCert 确认上传的是合法 PEM，并且私钥和证书匹配。
func parseCert(certPEM, keyPEM string) (*x509.Certificate, error) {
	if _, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM)); err != nil {
		return nil, httpx.Invalid("证书和私钥不匹配，或不是合法的 PEM")
	}
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return nil, httpx.Invalid("证书不是合法的 PEM")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, httpx.Invalid("证书无法解析")
	}
	return leaf, nil
}

// certNames 收集证书的域名，供界面展示。
func certNames(cert *x509.Certificate) []string {
	names := make([]string, 0, 1+len(cert.DNSNames))
	if cert.Subject.CommonName != "" {
		names = append(names, cert.Subject.CommonName)
	}
	names = append(names, cert.DNSNames...)
	for _, ip := range cert.IPAddresses {
		names = append(names, ip.String())
	}
	return unique(names)
}

// unique 去掉重复域名，保持原来的顺序。
func unique(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
