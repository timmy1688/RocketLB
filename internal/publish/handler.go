// 本文件是预览、发布、回滚、安装和历史记录的 HTTP 入口。
package publish

import (
	"github.com/gin-gonic/gin"

	"rocketlb/internal/httpx"
)

// Handler 把预览、发布、回滚、安装和历史查询接到 HTTP。
type Handler struct{ svc *Service }

// NewHandler 在启动时调用一次。
func NewHandler(svc *Service) Handler { return Handler{svc: svc} }

// Preview 处理 GET /api/v1/clusters/:id/preview。
func (h Handler) Preview(c *gin.Context) {
	bundle, err := h.svc.Preview(c.Request.Context(), c.Param("id"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, bundle)
}

// Publish 处理 POST /api/v1/clusters/:id/publish，返回 202。
func (h Handler) Publish(c *gin.Context) {
	job, err := h.svc.StartPublish(c.Param("id"), c.GetString("username"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.Accepted(c, job)
}

// Provision 处理 POST /api/v1/clusters/:id/provision，返回 202。先安装软件，再发布当前配置。
func (h Handler) Provision(c *gin.Context) {
	job, err := h.svc.StartProvision(c.Param("id"), c.GetString("username"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.Accepted(c, job)
}

// Rollback 处理 POST /api/v1/clusters/:id/revisions/:revisionId/rollback，返回 202。版本编号在路径里。
func (h Handler) Rollback(c *gin.Context) {
	job, err := h.svc.StartRollback(c.Param("id"), c.Param("revisionId"), c.GetString("username"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.Accepted(c, job)
}

// ListJobs 处理 GET /api/v1/clusters/:id/jobs。
func (h Handler) ListJobs(c *gin.Context) {
	items, err := h.svc.ListJobs(c.Request.Context(), c.Param("id"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, items)
}

// GetJob 处理 GET /api/v1/jobs/:id。
func (h Handler) GetJob(c *gin.Context) {
	item, err := h.svc.GetJob(c.Request.Context(), c.Param("id"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, item)
}

// ListRevisions 处理 GET /api/v1/clusters/:id/revisions。
func (h Handler) ListRevisions(c *gin.Context) {
	items, err := h.svc.ListRevisions(c.Request.Context(), c.Param("id"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, items)
}

// GetRevision 处理 GET /api/v1/revisions/:id。
func (h Handler) GetRevision(c *gin.Context) {
	item, err := h.svc.GetRevision(c.Request.Context(), c.Param("id"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, item)
}
