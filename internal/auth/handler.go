package auth

// handler 把 HTTP 和 Service 隔开：这里只解析 JSON、调用业务、按信封写回。

import (
	"github.com/gin-gonic/gin"

	"rocketlb/internal/httpx"
)

// credentials 是登录请求体。字段名跟前端 JSON 的 username、password 对应。
type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Handler 挂在 Gin 路由上。路由在 internal/app/app.go 的 mount 里登记。
type Handler struct {
	svc *Service
}

// NewHandler 在启动时创建一次，之后每个请求复用同一个实例。
func NewHandler(svc *Service) Handler {
	return Handler{svc: svc}
}

// Login 处理 POST /api/v1/login。这条路由没有登录中间件，否则没人能拿到令牌。
func (h Handler) Login(c *gin.Context) {
	var body credentials
	if err := httpx.Decode(c, &body); err != nil {
		httpx.Fail(c, err)
		return
	}
	result, err := h.svc.Login(c.Request.Context(), body.Username, body.Password)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, result)
}

// Password 处理 POST /api/v1/password，只改当前登录用户自己的密码。
func (h Handler) Password(c *gin.Context) {
	var body struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	}
	if err := httpx.Decode(c, &body); err != nil {
		httpx.Fail(c, err)
		return
	}
	user, err := h.svc.ChangePassword(c.Request.Context(), c.GetString("userID"), body.Current, body.Next)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, user)
}

// Me 处理 GET /api/v1/me。前端拿它判断当前是管理员还是只读账号。
func (h Handler) Me(c *gin.Context) {
	user, err := h.svc.Me(c.Request.Context(), c.GetString("userID"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, user)
}
