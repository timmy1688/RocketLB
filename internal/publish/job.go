package publish

import (
	"strings"
	"time"

	"gorm.io/gorm"

	"rocketlb/internal/model"
)

// maxLog 是一条任务日志的上限，64KiB。再长就只留尾部。
const maxLog = 64 << 10

// JobView 是返回给前端的任务。时间用 time.Time，Gin 会把它编成 RFC3339 字符串。
type JobView struct {
	ID         string     `json:"id"`
	ClusterID  string     `json:"clusterId"`
	RevisionID string     `json:"revisionId,omitempty"`
	Action     string     `json:"action"`
	Status     string     `json:"status"`
	Log        string     `json:"log"`
	CreatedBy  string     `json:"createdBy"`
	CreatedAt  time.Time  `json:"createdAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// toJobView 把数据库行转成接口字段。空的 RevisionID 不会出现在 JSON 里。
func toJobView(job model.DeployJob) JobView {
	return JobView{
		ID:         job.ID,
		ClusterID:  job.ClusterID,
		RevisionID: job.RevisionID,
		Action:     job.Action,
		Status:     job.Status,
		Log:        job.Log,
		CreatedBy:  job.CreatedBy,
		CreatedAt:  job.CreatedAt,
		FinishedAt: job.FinishedAt,
	}
}

// appendLog 给还在跑的任务追加一行。用事务先读再写，避免两条日志互相覆盖。
func appendLog(db *gorm.DB, id, line string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var job model.DeployJob
		if err := tx.First(&job, "id = ?", id).Error; err != nil {
			return err
		}
		return tx.Model(&job).Update("log", joinLog(job.Log, line)).Error
	})
}

// finishJob 把任务收成成功或失败，并记下结束时间。已经结束的任务不会被改第二次。
func finishJob(db *gorm.DB, id, status, line string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var job model.DeployJob
		if err := tx.First(&job, "id = ?", id).Error; err != nil {
			return err
		}
		if job.Status != model.JobRunning {
			return nil
		}
		now := time.Now()
		return tx.Model(&job).Updates(map[string]any{
			"status":      status,
			"log":         joinLog(job.Log, line),
			"finished_at": &now,
		}).Error
	})
}

// loadJob 按编号取一条任务，供前端轮询。
func loadJob(db *gorm.DB, id string) (JobView, error) {
	var job model.DeployJob
	if err := db.First(&job, "id = ?", id).Error; err != nil {
		return JobView{}, err
	}
	return toJobView(job), nil
}

// loadJobs 返回一个集群最近的任务，新的在前。
func loadJobs(db *gorm.DB, clusterID string) ([]JobView, error) {
	var jobs []model.DeployJob
	err := db.Where("cluster_id = ?", clusterID).Order("created_at desc").Find(&jobs).Error
	if err != nil {
		return nil, err
	}
	out := make([]JobView, 0, len(jobs))
	for _, job := range jobs {
		out = append(out, toJobView(job))
	}
	return out, nil
}

// FailInterrupted 在启动时把上次没做完的任务标成失败。
// 发布和安装在后台跑，进程一旦重启，那些任务不会自己继续。
func FailInterrupted(db *gorm.DB) error {
	var jobs []model.DeployJob
	if err := db.Where("status = ?", model.JobRunning).Find(&jobs).Error; err != nil {
		return err
	}
	for _, job := range jobs {
		if err := finishJob(db, job.ID, model.JobFailed, "控制面重启，任务中断"); err != nil {
			return err
		}
	}
	return db.Model(&model.Revision{}).Where("status = ?", model.RevisionPublishing).Update("status", model.RevisionFailed).Error
}

// joinLog 把新的一行接到旧日志后面。日志最长保留 64KiB，超出时丢掉最旧的部分。
func joinLog(current, line string) string {
	line = strings.TrimRight(line, "\n")
	if line == "" {
		return trimLog(current)
	}
	if current == "" {
		return trimLog(line + "\n")
	}
	if !strings.HasSuffix(current, "\n") {
		current += "\n"
	}
	return trimLog(current + line + "\n")
}

// trimLog 只保留日志尾部，避免一次长时间发布把数据库行撑得太大。
func trimLog(s string) string {
	if len(s) <= maxLog {
		return s
	}
	return s[len(s)-maxLog:]
}
