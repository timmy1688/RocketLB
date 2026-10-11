// Package publish 负责预览、发布、回滚和一键安装。
// 接口先返回任务，SSH 在后台做：渲染、备份、nginx -t，全部通过才 reload，失败则还原。
// 安装脚本、任务日志和集群锁也放在这里，它们只为这条流程服务。
package publish

import (
	"context"
	"encoding/json"
	"fmt"
	"rocketlb/internal/httpx"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"rocketlb/internal/model"
	"rocketlb/internal/node"
	"rocketlb/internal/render"
	"rocketlb/internal/secret"
	"rocketlb/internal/sshclient"
	"rocketlb/internal/store"
)

// Revision 是一次发布留在数据库里的版本摘要，不含文件正文。
type Revision struct {
	ID        string    `json:"id"`
	ClusterID string    `json:"clusterId"`
	Hash      string    `json:"hash"`
	Status    string    `json:"status"`
	Note      string    `json:"note"`
	CreatedBy string    `json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`
}

// RevisionDetail 在摘要之外带上文件内容。私钥已经打码。
type RevisionDetail struct {
	Revision
	Files []render.File `json:"files"`
}

// Node 是发布时要用的一台成员，Spec 里已经放好解密后的 SSH 凭据。
type Node struct {
	ID       string
	Name     string
	Address  string
	Priority int
	Role     string
	Spec     sshclient.Spec
}

// Service 负责预览、发布和回滚。它不自己解析 HTTP，handler 负责接请求。
type Service struct {
	db      *gorm.DB
	box     *secret.Box
	ssh     sshclient.Runner
	timeout time.Duration
}

// NewService 在启动时创建。timeout 是单次 SSH 的超时，整次发布另有自己的截止时间。
func NewService(db *gorm.DB, box *secret.Box, ssh sshclient.Runner, timeout time.Duration) *Service {
	return &Service{db: db, box: box, ssh: ssh, timeout: timeout}
}

// Preview 渲染当前期望配置并藏起私钥，不连节点。
func (s *Service) Preview(ctx context.Context, clusterID string) (render.Bundle, error) {
	bundle, err := s.Bundle(ctx, clusterID)
	if err != nil {
		return render.Bundle{}, err
	}
	return bundle.Redacted(), nil
}

// StartPublish 先确认渲染能通过，再后台执行发布，接口马上返回任务。
func (s *Service) StartPublish(clusterID, user string) (JobView, error) {
	if _, err := s.Bundle(context.Background(), clusterID); err != nil {
		return JobView{}, err
	}
	return s.start(clusterID, user, model.ActionPublish, 2*time.Minute, func(ctx context.Context, jobID string) error {
		return s.Apply(ctx, jobID, clusterID, user, "发布")
	})
}

// StartRollback 取出某个已发布版本的文件，再按同样的预检和重载流程写回去。
func (s *Service) StartRollback(clusterID, revisionID, user string) (JobView, error) {
	rev, files, err := s.publishedFiles(context.Background(), clusterID, revisionID)
	if err != nil {
		return JobView{}, err
	}
	bundle := render.Bundle{Files: files, Hash: rev.Hash}
	return s.start(clusterID, user, model.ActionRollback, 2*time.Minute, func(ctx context.Context, jobID string) error {
		note := "回滚到 " + rev.ID
		return s.applyBundle(ctx, jobID, clusterID, user, note, bundle)
	})
}

// Apply 按数据库里的期望配置渲染并写到节点。调用方必须已经拿着这个集群的锁。安装完成后会直接调它。
func (s *Service) Apply(ctx context.Context, jobID, clusterID, user, note string) error {
	bundle, err := s.Bundle(ctx, clusterID)
	if err != nil {
		return err
	}
	return s.applyBundle(ctx, jobID, clusterID, user, note, bundle)
}

// ListJobs 返回这个集群的任务列表，给界面的任务页用。
func (s *Service) ListJobs(ctx context.Context, clusterID string) ([]JobView, error) {
	if err := s.ensureCluster(ctx, clusterID); err != nil {
		return nil, err
	}
	return loadJobs(s.db.WithContext(ctx), clusterID)
}

// GetJob 返回一条任务的最新日志。前端在 202 之后轮询它。
func (s *Service) GetJob(ctx context.Context, id string) (JobView, error) {
	view, err := loadJob(s.db.WithContext(ctx), id)
	if err != nil {
		return JobView{}, store.NotFound(err, "任务不存在")
	}
	return view, nil
}

// ListRevisions 返回发布历史，新的在前。
func (s *Service) ListRevisions(ctx context.Context, clusterID string) ([]Revision, error) {
	if err := s.ensureCluster(ctx, clusterID); err != nil {
		return nil, err
	}
	var rows []model.Revision
	if err := s.db.WithContext(ctx).Where("cluster_id = ?", clusterID).Order("created_at desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Revision, 0, len(rows))
	for _, row := range rows {
		out = append(out, revisionView(row))
	}
	return out, nil
}

// GetRevision 返回一个版本的文件预览，私钥打码。
func (s *Service) GetRevision(ctx context.Context, id string) (RevisionDetail, error) {
	var row model.Revision
	if err := s.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		return RevisionDetail{}, store.NotFound(err, "版本不存在")
	}
	files, err := s.decodeFiles(row)
	if err != nil {
		return RevisionDetail{}, err
	}
	redacted := (render.Bundle{Files: files, Hash: row.Hash}).Redacted()
	return RevisionDetail{Revision: revisionView(row), Files: redacted.Files}, nil
}

// Nodes 读出集群成员并解密 SSH 凭据。安装和发布都用这份名单。
func (s *Service) Nodes(ctx context.Context, clusterID string) ([]Node, error) {
	// row 只在这个函数里用来接 SQL 联表结果，不是数据库表。
	type row struct {
		NodeID    string
		Name      string
		Address   string
		SSHPort   int
		SSHUser   string
		AuthType  string
		SecretEnc string
		HostKey   string
		Priority  int
		Role      string
	}
	var rows []row
	err := s.db.WithContext(ctx).Table("cluster_members").
		Select("cluster_members.node_id, cluster_members.priority, cluster_members.role, nodes.name, nodes.address, nodes.ssh_port, nodes.ssh_user, nodes.auth_type, nodes.secret_enc, nodes.host_key").
		Joins("join nodes on nodes.id = cluster_members.node_id").
		Where("cluster_members.cluster_id = ?", clusterID).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]Node, 0, len(rows))
	for _, item := range rows {
		spec, err := node.Spec(s.box, model.Node{
			ID: item.NodeID, Name: item.Name, Address: item.Address, SSHPort: item.SSHPort,
			SSHUser: item.SSHUser, AuthType: item.AuthType, SecretEnc: item.SecretEnc, HostKey: item.HostKey,
		}, s.timeout)
		if err != nil {
			return nil, err
		}
		out = append(out, Node{
			ID: item.NodeID, Name: item.Name, Address: item.Address, Priority: item.Priority, Role: item.Role, Spec: spec,
		})
	}
	return out, nil
}

// NoteHostKey 在第一次连上时把主机公钥记下来。已经有记录就不会覆盖。
func (s *Service) NoteHostKey(id, key string) {
	if key == "" || id == "" {
		return
	}
	s.db.Model(&model.Node{}).Where("id = ? AND (host_key = '' OR host_key IS NULL)", id).Update("host_key", key)
}

// start 抢锁、写入 running 任务，然后在后台跑 work。锁要等后台结束才释放，所以接口返回后别人仍不能对同一集群再点一次。
func (s *Service) start(clusterID, user, action string, timeout time.Duration, work func(context.Context, string) error) (JobView, error) {
	if err := s.ensureCluster(context.Background(), clusterID); err != nil {
		return JobView{}, err
	}
	unlock, ok := lockCluster(clusterID)
	if !ok {
		return JobView{}, httpx.Conflict("集群正在执行安装或发布")
	}
	job := model.DeployJob{
		ID:        model.NewID(),
		ClusterID: clusterID,
		Action:    action,
		Status:    model.JobRunning,
		CreatedBy: user,
	}
	if err := s.db.Create(&job).Error; err != nil {
		unlock()
		return JobView{}, err
	}
	go func() {
		defer unlock()
		defer func() {
			if rec := recover(); rec != nil {
				_ = finishJob(s.db, job.ID, model.JobFailed, fmt.Sprintf("内部错误: %v", rec))
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		err := work(ctx, job.ID)
		if err != nil {
			_ = finishJob(s.db, job.ID, model.JobFailed, err.Error())
			return
		}
		_ = finishJob(s.db, job.ID, model.JobSucceeded, "完成")
	}()
	_ = s.db.First(&job, "id = ?", job.ID)
	return toJobView(job), nil
}

// applyBundle 先把文件快照加密存成版本，再推到节点。推失败会把版本标成失败。
func (s *Service) applyBundle(ctx context.Context, jobID, clusterID, user, note string, bundle render.Bundle) error {
	nodes, err := s.Nodes(ctx, clusterID)
	if err != nil {
		return err
	}
	if err := covers(bundle.Files, nodes); err != nil {
		return err
	}
	raw, err := json.Marshal(bundle.Files)
	if err != nil {
		return err
	}
	enc, err := s.box.Encrypt(string(raw))
	if err != nil {
		return err
	}
	rev := model.Revision{
		ID:          model.NewID(),
		ClusterID:   clusterID,
		Hash:        bundle.Hash,
		Status:      model.RevisionPublishing,
		Note:        note,
		CreatedBy:   user,
		ManifestEnc: enc,
	}
	if err := s.db.WithContext(ctx).Create(&rev).Error; err != nil {
		return err
	}
	if err := s.db.Model(&model.DeployJob{}).Where("id = ?", jobID).Update("revision_id", rev.ID).Error; err != nil {
		return err
	}
	if err := s.push(ctx, jobID, rev.ID, bundle.Files, nodes); err != nil {
		_ = s.db.Model(&model.Revision{}).Where("id = ?", rev.ID).Update("status", model.RevisionFailed).Error
		return err
	}
	return s.db.Model(&model.Revision{}).Where("id = ?", rev.ID).Update("status", model.RevisionPublished).Error
}

// push 分两段。先让每台节点备份并 nginx -t，有一台失败就还原已经写过的机器，谁都不 reload。全部通过后才逐台激活。
func (s *Service) push(ctx context.Context, jobID, backupID string, files []render.File, nodes []Node) error {
	prepared := make([]Node, 0, len(nodes))
	for _, item := range nodes {
		script, err := prepareScript(backupID, nodeFiles(files, item.ID))
		if err != nil {
			return err
		}
		_ = appendLog(s.db, jobID, item.Name+" 写入配置并预检")
		res, err := s.ssh.Run(ctx, item.Spec, script, true)
		s.NoteHostKey(item.ID, res.HostKey)
		if err != nil {
			s.restore(ctx, jobID, backupID, prepared)
			return fmt.Errorf("%s 预检失败: %s", item.Name, errText(err, res))
		}
		prepared = append(prepared, item)
		_ = appendLog(s.db, jobID, item.Name+" 预检通过")
	}
	sort.SliceStable(prepared, func(i, j int) bool {
		return hasPath(nodeFiles(files, prepared[i].ID), "/etc/nginx/nginx.conf") && !hasPath(nodeFiles(files, prepared[j].ID), "/etc/nginx/nginx.conf")
	})
	for _, item := range prepared {
		res, err := s.ssh.Run(ctx, item.Spec, activateScript(nodeFiles(files, item.ID)), true)
		s.NoteHostKey(item.ID, res.HostKey)
		if err != nil {
			s.restore(ctx, jobID, backupID, prepared)
			return fmt.Errorf("%s 重载失败: %s", item.Name, errText(err, res))
		}
		_ = appendLog(s.db, jobID, item.Name+" 已重载")
	}
	return nil
}

// restore 对已经预检过的节点执行还原。某一台还原失败只记日志，继续处理下一台。
func (s *Service) restore(ctx context.Context, jobID, backupID string, nodes []Node) {
	script, err := restoreScript(backupID)
	if err != nil {
		_ = appendLog(s.db, jobID, "无法生成回滚脚本")
		return
	}
	for _, item := range nodes {
		res, err := s.ssh.Run(ctx, item.Spec, script, true)
		if err != nil {
			_ = appendLog(s.db, jobID, fmt.Sprintf("%s 回滚失败: %s", item.Name, errText(err, res)))
			continue
		}
		_ = appendLog(s.db, jobID, item.Name+" 已回滚到发布前的配置")
	}
}

// publishedFiles 只允许回滚状态是已发布的版本。
func (s *Service) publishedFiles(ctx context.Context, clusterID, revisionID string) (model.Revision, []render.File, error) {
	var rev model.Revision
	err := s.db.WithContext(ctx).Where("id = ? AND cluster_id = ?", revisionID, clusterID).First(&rev).Error
	if err != nil {
		return model.Revision{}, nil, store.NotFound(err, "版本不存在")
	}
	if rev.Status != model.RevisionPublished {
		return model.Revision{}, nil, httpx.Invalid("只能回滚已经发布成功的版本")
	}
	files, err := s.decodeFiles(rev)
	if err != nil {
		return model.Revision{}, nil, err
	}
	return rev, files, nil
}

// decodeFiles 解密版本里保存的文件 JSON。
func (s *Service) decodeFiles(rev model.Revision) ([]render.File, error) {
	plain, err := s.box.Decrypt(rev.ManifestEnc)
	if err != nil {
		return nil, err
	}
	var files []render.File
	if err := json.Unmarshal([]byte(plain), &files); err != nil {
		return nil, err
	}
	return files, nil
}

// ensureCluster 确认集群还在，避免给一个已删除的编号列任务。
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

// covers 确认每台当前成员至少有 nginx 或 keepalived。回滚到成员集合已经对不上的旧版本时会在这里停下。
func covers(files []render.File, nodes []Node) error {
	for _, item := range nodes {
		if !nodeHas(files, item.ID, "/etc/keepalived/keepalived.conf") && !nodeHas(files, item.ID, "/etc/nginx/nginx.conf") {
			return httpx.Invalidf("版本里没有节点 %s 的配置", item.Name)
		}
	}
	return nil
}

func nodeHas(files []render.File, nodeID, path string) bool {
	for _, file := range files {
		if file.Path == path && (file.NodeID == "" || file.NodeID == nodeID) {
			return true
		}
	}
	return false
}

// nodeFiles 选出这台节点要写的文件：大家共用的，加上 NodeID 等于它自己的。
func nodeFiles(files []render.File, nodeID string) []scriptFile {
	out := make([]scriptFile, 0, len(files))
	for _, file := range files {
		if file.NodeID != "" && file.NodeID != nodeID {
			continue
		}
		out = append(out, scriptFile{Path: file.Path, Content: file.Content, Mode: file.Mode})
	}
	return out
}

// errText 把 SSH 错误和远端 stderr 合成一行，写进任务日志。
func errText(err error, res sshclient.Result) string {
	msg := strings.TrimSpace(err.Error())
	extra := strings.TrimSpace(res.Stderr)
	if extra != "" && !strings.Contains(msg, extra) {
		msg += ": " + extra
	}
	return msg
}

// revisionView 去掉加密正文，只留版本摘要。
func revisionView(row model.Revision) Revision {
	return Revision{
		ID: row.ID, ClusterID: row.ClusterID, Hash: row.Hash, Status: row.Status,
		Note: row.Note, CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt,
	}
}
