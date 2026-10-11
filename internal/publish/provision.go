package publish

import (
	"context"
	"fmt"
	"time"

	"rocketlb/internal/model"
)

// StartProvision 先确认配置能渲染，再后台安装 nginx 和 keepalived，最后走一遍普通发布。
// 接口马上返回任务。软件来自系统源，整次安装最多 15 分钟。
func (s *Service) StartProvision(clusterID, user string) (JobView, error) {
	if _, err := s.Bundle(context.Background(), clusterID); err != nil {
		return JobView{}, err
	}
	return s.start(clusterID, user, model.ActionProvision, 15*time.Minute, func(ctx context.Context, jobID string) error {
		return s.install(ctx, jobID, clusterID, user)
	})
}

// install 逐台执行安装脚本。一台失败就停止，已经装上的软件留在节点上。全部成功后再发布。
func (s *Service) install(ctx context.Context, jobID, clusterID, user string) error {
	nodes, err := s.Nodes(ctx, clusterID)
	if err != nil {
		return err
	}
	for _, item := range nodes {
		_ = appendLog(s.db, jobID, item.Name+" "+installText(item.Role))
		res, err := s.ssh.Run(ctx, item.Spec, installScriptFor(installRole(item.Role)), true)
		s.NoteHostKey(item.ID, res.HostKey)
		if err != nil {
			return fmt.Errorf("%s 安装失败: %s", item.Name, err.Error())
		}
		_ = appendLog(s.db, jobID, item.Name+" 安装完成")
	}
	return s.Apply(ctx, jobID, clusterID, user, "安装后发布")
}

func installRole(role string) string {
	switch role {
	case "lvs", "nginx":
		return role
	default:
		return "both"
	}
}

func installText(role string) string {
	switch installRole(role) {
	case "lvs":
		return "开始安装 keepalived，并调整系统参数"
	case "nginx":
		return "开始安装 nginx，并调整系统参数"
	default:
		return "开始安装 nginx、keepalived，并调整系统参数"
	}
}
