// Package app 把控制面组装起来并提供 HTTP 服务。
//
// 一次请求的路径是：Gin 收到 HTTP → 登录中间件核对令牌 → 对应的 handler 调业务包 →
// 业务包读写数据库或通过 SSH 操作节点 → httpx 把结果包成 {"data": ...} 或 {"error": ...}。
// 浏览器页面由 mountUI 从 web/dist 直接送出，接口都在 /api/v1 下面。
package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"rocketlb/internal/auth"
	"rocketlb/internal/cluster"
	"rocketlb/internal/config"
	"rocketlb/internal/node"
	"rocketlb/internal/proxy"
	"rocketlb/internal/publish"
	"rocketlb/internal/secret"
	"rocketlb/internal/sshclient"
	"rocketlb/internal/store"
)

// Version 会出现在 /healthz 和启动日志里，用来确认跑的是哪一版。
const Version = "0.1.0"

// App 是已经组装好的控制面。Engine 负责 HTTP，DB 是打开的数据库连接。
type App struct {
	Config config.Config
	Engine *gin.Engine
	DB     *gorm.DB
}

// New 按配置把数据库、加密、登录和各业务模块接在一起。这里还不会监听端口，监听在 Run。
func New(cfg config.Config) (*App, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.Env == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	db, err := store.Open(cfg)
	if err != nil {
		return nil, err
	}
	if err := store.Migrate(db); err != nil {
		return nil, err
	}
	if err := publish.FailInterrupted(db); err != nil {
		return nil, err
	}
	box, err := secret.New(cfg.DataKey)
	if err != nil {
		return nil, err
	}
	tokens := auth.NewTokens(cfg.JWTSecret)
	authSvc := auth.NewService(db, tokens)
	if err := authSvc.EnsureAdmin(cfg.AdminUser, cfg.AdminPassword); err != nil {
		return nil, err
	}
	if cfg.AdminPassword == "admin" {
		slog.Warn("管理员密码仍是默认值，请在首次启动前修改 ROCKETLB_ADMIN_PASSWORD")
	}
	remote := sshclient.Remote{}
	nodes := node.NewService(db, box, remote, cfg.SSHDialTimeout)
	clusters := cluster.NewService(db, box)
	proxies := proxy.NewService(db, box)
	pubs := publish.NewService(db, box, remote, cfg.SSHDialTimeout)

	engine := gin.New()
	engine.Use(gin.Recovery(), limitBody(2<<20))
	mount(engine, handlers{
		tokens:  tokens,
		auth:    auth.NewHandler(authSvc),
		node:    node.NewHandler(nodes),
		cluster: cluster.NewHandler(clusters),
		proxy:   proxy.NewHandler(proxies),
		publish: publish.NewHandler(pubs),
	})
	mountUI(engine, cfg.WebDist)
	return &App{Config: cfg, Engine: engine, DB: db}, nil
}

// Run 在 HTTPAddr 上监听。收到 SIGINT 或 SIGTERM（service.sh stop 发的就是这个）后，最多等 10 秒把已接受的请求处理完再退出。
func (a *App) Run() error {
	addr := a.Config.HTTPAddr
	if addr == "" {
		addr = ":8080"
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           a.Engine,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       time.Minute,
		WriteTimeout:      time.Minute,
		IdleTimeout:       time.Minute,
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", addr, "version", Version)
		errCh <- srv.ListenAndServe()
	}()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdown)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// handlers 把各业务的 HTTP 入口收在一处，方便 mount 注册路由。
type handlers struct {
	tokens  *auth.Tokens
	auth    auth.Handler
	node    node.Handler
	cluster cluster.Handler
	proxy   proxy.Handler
	publish publish.Handler
}

// mount 注册全部接口。
// /healthz 和登录不需要令牌。authed 下面的接口要带 Authorization: Bearer。
// write 再要求角色不是 viewer，只读账号只能看不能改。
func mount(r *gin.Engine, h handlers) {
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "version": Version})
	})
	v1 := r.Group("/api/v1")
	v1.POST("/login", h.auth.Login)

	authed := v1.Group("")
	authed.Use(h.tokens.Middleware())
	authed.GET("/me", h.auth.Me)
	authed.POST("/password", h.auth.Password)
	authed.GET("/nodes", h.node.List)
	authed.GET("/nodes/:id", h.node.Get)
	authed.GET("/clusters", h.cluster.List)
	authed.GET("/clusters/:id", h.cluster.Get)
	authed.GET("/clusters/:id/upstreams", h.proxy.ListUpstreams)
	authed.GET("/clusters/:id/sites", h.proxy.ListSites)
	authed.GET("/clusters/:id/streams", h.proxy.ListStreams)
	authed.GET("/clusters/:id/certificates", h.proxy.ListCertificates)
	authed.GET("/clusters/:id/preview", h.publish.Preview)
	authed.GET("/clusters/:id/jobs", h.publish.ListJobs)
	authed.GET("/clusters/:id/revisions", h.publish.ListRevisions)
	authed.GET("/jobs/:id", h.publish.GetJob)
	authed.GET("/revisions/:id", h.publish.GetRevision)

	write := authed.Group("")
	write.Use(auth.RequireWriter())
	write.POST("/nodes", h.node.Create)
	write.PUT("/nodes/:id", h.node.Update)
	write.DELETE("/nodes/:id", h.node.Delete)
	write.POST("/nodes/:id/probe", h.node.Probe)
	write.POST("/clusters", h.cluster.Create)
	write.PUT("/clusters/:id", h.cluster.Update)
	write.DELETE("/clusters/:id", h.cluster.Delete)
	write.POST("/clusters/:id/upstreams", h.proxy.CreateUpstream)
	write.PUT("/upstreams/:id", h.proxy.UpdateUpstream)
	write.DELETE("/upstreams/:id", h.proxy.DeleteUpstream)
	write.POST("/clusters/:id/sites", h.proxy.CreateSite)
	write.PUT("/sites/:id", h.proxy.UpdateSite)
	write.DELETE("/sites/:id", h.proxy.DeleteSite)
	write.POST("/clusters/:id/streams", h.proxy.CreateStream)
	write.PUT("/streams/:id", h.proxy.UpdateStream)
	write.DELETE("/streams/:id", h.proxy.DeleteStream)
	write.POST("/clusters/:id/certificates", h.proxy.CreateCertificate)
	write.DELETE("/certificates/:id", h.proxy.DeleteCertificate)
	write.POST("/clusters/:id/publish", h.publish.Publish)
	write.POST("/clusters/:id/revisions/:revisionId/rollback", h.publish.Rollback)
	write.POST("/clusters/:id/provision", h.publish.Provision)
}

// limitBody 限制单个请求体大小，避免把证书或超大 JSON 直接读进内存。n 的单位是字节，2<<20 即 2MiB。
func limitBody(n int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, n)
		c.Next()
	}
}

// mountUI 在没有匹配到接口时返回前端页面。
// 真实文件存在就返回该文件，否则返回 index.html，让浏览器里的 Vue 路由接管。
// /api 下找不到的路径仍返回 JSON 404，避免把接口错误显示成网页。
func mountUI(r *gin.Engine, dist string) {
	if dist == "" {
		return
	}
	root := filepath.Clean(dist)
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") || c.Request.URL.Path == "/api" {
			c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "not_found", "message": "接口不存在"}})
			return
		}
		rel := strings.TrimPrefix(filepath.Clean("/"+c.Request.URL.Path), "/")
		full := filepath.Join(root, rel)
		if rel != "" && !strings.HasPrefix(full, root+string(filepath.Separator)) && full != root {
			c.Status(http.StatusForbidden)
			return
		}
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			c.File(full)
			return
		}
		c.File(filepath.Join(root, "index.html"))
	})
}
