// Package httpx 统一接口的返回格式。成功是 {"data": ...}，失败是 {"error": {"code","message"}}。
// 前端只认这一种信封，所以每个 handler 都通过这里写响应，而不是自己拼 JSON。
package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

// OK 返回 200。查询、更新和探测用它。
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"data": data})
}

// Created 返回 201。新建节点、集群、站点和证书用它。
func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, gin.H{"data": data})
}

// Accepted 返回 202。发布、回滚和安装不会当场做完，先把任务编号交回去，前端再轮询任务。
func Accepted(c *gin.Context, data any) {
	c.JSON(http.StatusAccepted, gin.H{"data": data})
}

// Fail 把业务错误按它自带的状态码返回。不是 Error 的错误当成内部错误，详细原因只写日志，不给浏览器。
func Fail(c *gin.Context, err error) {
	var ae *Error
	if errors.As(err, &ae) {
		c.JSON(ae.Status, gin.H{"error": gin.H{"code": ae.Code, "message": ae.Message}})
		return
	}
	slog.Error("request failed", "err", err, "path", c.Request.URL.Path)
	c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{
		"code":    "internal",
		"message": "内部错误",
	}})
}

// Decode 把请求 JSON 填进 dst。多出来的字段会拒绝，避免前端拼错字段名还被悄悄丢掉。
func Decode(c *gin.Context, dst any) error {
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return Invalid("请求体为空")
		}
		return Invalid("请求体不是合法的 JSON")
	}
	return nil
}
