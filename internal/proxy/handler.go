// 本文件是上游、站点、四层代理和证书的 HTTP 入口。
package proxy

import (
	"github.com/gin-gonic/gin"

	"rocketlb/internal/httpx"
)

// Handler 把上游、站点、四层代理和证书的 HTTP 路径接到 Service。
type Handler struct{ svc *Service }

// NewHandler 在启动时调用一次。
func NewHandler(svc *Service) Handler { return Handler{svc: svc} }

// ListUpstreams 处理 GET /api/v1/clusters/:id/upstreams。
func (h Handler) ListUpstreams(c *gin.Context) {
	items, err := h.svc.ListUpstreams(c.Request.Context(), c.Param("id"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, items)
}

// CreateUpstream 处理 POST /api/v1/clusters/:id/upstreams。
func (h Handler) CreateUpstream(c *gin.Context) {
	var body UpstreamWrite
	if err := httpx.Decode(c, &body); err != nil {
		httpx.Fail(c, err)
		return
	}
	item, err := h.svc.CreateUpstream(c.Request.Context(), c.Param("id"), body)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.Created(c, item)
}

// UpdateUpstream 处理 PUT /api/v1/upstreams/:id。
func (h Handler) UpdateUpstream(c *gin.Context) {
	var body UpstreamWrite
	if err := httpx.Decode(c, &body); err != nil {
		httpx.Fail(c, err)
		return
	}
	item, err := h.svc.UpdateUpstream(c.Request.Context(), c.Param("id"), body)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, item)
}

// DeleteUpstream 处理 DELETE /api/v1/upstreams/:id。
func (h Handler) DeleteUpstream(c *gin.Context) {
	if err := h.svc.DeleteUpstream(c.Request.Context(), c.Param("id")); err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{"ok": true})
}

// ListSites 处理 GET /api/v1/clusters/:id/sites。
func (h Handler) ListSites(c *gin.Context) {
	items, err := h.svc.ListSites(c.Request.Context(), c.Param("id"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, items)
}

// CreateSite 处理 POST /api/v1/clusters/:id/sites。
func (h Handler) CreateSite(c *gin.Context) {
	var body SiteWrite
	if err := httpx.Decode(c, &body); err != nil {
		httpx.Fail(c, err)
		return
	}
	item, err := h.svc.CreateSite(c.Request.Context(), c.Param("id"), body)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.Created(c, item)
}

// UpdateSite 处理 PUT /api/v1/sites/:id。
func (h Handler) UpdateSite(c *gin.Context) {
	var body SiteWrite
	if err := httpx.Decode(c, &body); err != nil {
		httpx.Fail(c, err)
		return
	}
	item, err := h.svc.UpdateSite(c.Request.Context(), c.Param("id"), body)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, item)
}

// DeleteSite 处理 DELETE /api/v1/sites/:id。
func (h Handler) DeleteSite(c *gin.Context) {
	if err := h.svc.DeleteSite(c.Request.Context(), c.Param("id")); err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{"ok": true})
}

// ListStreams 处理 GET /api/v1/clusters/:id/streams。
func (h Handler) ListStreams(c *gin.Context) {
	items, err := h.svc.ListStreams(c.Request.Context(), c.Param("id"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, items)
}

// CreateStream 处理 POST /api/v1/clusters/:id/streams。
func (h Handler) CreateStream(c *gin.Context) {
	var body StreamWrite
	if err := httpx.Decode(c, &body); err != nil {
		httpx.Fail(c, err)
		return
	}
	item, err := h.svc.CreateStream(c.Request.Context(), c.Param("id"), body)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.Created(c, item)
}

// UpdateStream 处理 PUT /api/v1/streams/:id。
func (h Handler) UpdateStream(c *gin.Context) {
	var body StreamWrite
	if err := httpx.Decode(c, &body); err != nil {
		httpx.Fail(c, err)
		return
	}
	item, err := h.svc.UpdateStream(c.Request.Context(), c.Param("id"), body)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, item)
}

// DeleteStream 处理 DELETE /api/v1/streams/:id。
func (h Handler) DeleteStream(c *gin.Context) {
	if err := h.svc.DeleteStream(c.Request.Context(), c.Param("id")); err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{"ok": true})
}

// ListCertificates 处理 GET /api/v1/clusters/:id/certificates。
func (h Handler) ListCertificates(c *gin.Context) {
	items, err := h.svc.ListCertificates(c.Request.Context(), c.Param("id"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, items)
}

// CreateCertificate 处理 POST /api/v1/clusters/:id/certificates。
func (h Handler) CreateCertificate(c *gin.Context) {
	var body CertificateWrite
	if err := httpx.Decode(c, &body); err != nil {
		httpx.Fail(c, err)
		return
	}
	item, err := h.svc.CreateCertificate(c.Request.Context(), c.Param("id"), body)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.Created(c, item)
}

// DeleteCertificate 处理 DELETE /api/v1/certificates/:id。
func (h Handler) DeleteCertificate(c *gin.Context) {
	if err := h.svc.DeleteCertificate(c.Request.Context(), c.Param("id")); err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{"ok": true})
}
