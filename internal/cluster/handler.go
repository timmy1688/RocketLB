// 本文件是集群接口的 HTTP 入口。
package cluster

import (
	"github.com/gin-gonic/gin"

	"rocketlb/internal/httpx"
)

// Handler 把 /api/v1/clusters 接到集群 Service。
type Handler struct{ svc *Service }

// NewHandler 在启动时调用一次。
func NewHandler(svc *Service) Handler { return Handler{svc: svc} }

// List 处理 GET /api/v1/clusters。
func (h Handler) List(c *gin.Context) {
	items, err := h.svc.List(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, items)
}

// Get 处理 GET /api/v1/clusters/:id。
func (h Handler) Get(c *gin.Context) {
	item, err := h.svc.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, item)
}

// Create 处理 POST /api/v1/clusters。
func (h Handler) Create(c *gin.Context) {
	var body Write
	if err := httpx.Decode(c, &body); err != nil {
		httpx.Fail(c, err)
		return
	}
	item, err := h.svc.Create(c.Request.Context(), body)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.Created(c, item)
}

// Update 处理 PUT /api/v1/clusters/:id。
func (h Handler) Update(c *gin.Context) {
	var body Write
	if err := httpx.Decode(c, &body); err != nil {
		httpx.Fail(c, err)
		return
	}
	item, err := h.svc.Update(c.Request.Context(), c.Param("id"), body)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, item)
}

// Delete 处理 DELETE /api/v1/clusters/:id。
func (h Handler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.Param("id")); err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{"ok": true})
}
