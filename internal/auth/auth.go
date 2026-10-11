// Package auth 管登录。密码用 bcrypt 存在用户表里，登录成功后发一张 12 小时的 JWT。
// 之后每个 /api 请求都带 Authorization: Bearer <token>，中间件验完把用户放进请求上下文。
package auth

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"rocketlb/internal/httpx"
	"rocketlb/internal/model"
	"rocketlb/internal/store"
)

// ttl 是登录令牌的有效期。过期后前端会收到 401，需要重新登录。
const ttl = 12 * time.Hour

// User 是返回给前端的账号，没有密码。
type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// LoginResult 是登录接口的成功响应：令牌加上当前用户。
type LoginResult struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

// claims 是写进 JWT 的内容。Subject 是用户编号，另外带上用户名和角色，中间件不用再查库。
type claims struct {
	jwt.RegisteredClaims
	Username string `json:"username"`
	Role     string `json:"role"`
}

// Tokens 用启动时读到的 JWT 密钥签发和校验令牌。
type Tokens struct {
	secret []byte
}

// NewTokens 保存密钥。密钥来自 data/jwt.secret，换掉它会使已发出的令牌全部失效。
func NewTokens(secret string) *Tokens {
	return &Tokens{secret: []byte(secret)}
}

// Issue 签发 HS256 令牌，12 小时后过期。
func (t *Tokens) Issue(user model.User) (string, error) {
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
		Username: user.Username,
		Role:     user.Role,
	})
	return token.SignedString(t.secret)
}

// Parse 验签名、算法和过期时间。算法不是 HS256 就拒绝，避免被换成 none。
func (t *Tokens) Parse(raw string) (claims, error) {
	var out claims
	token, err := jwt.ParseWithClaims(raw, &out, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, httpx.Unauthorized("登录已失效")
		}
		return t.secret, nil
	})
	if err != nil || !token.Valid {
		return claims{}, httpx.Unauthorized("登录已失效")
	}
	return out, nil
}

// Middleware 保护 /api。没有令牌或令牌无效时直接 401，后面的 handler 不会执行。
func (t *Tokens) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		raw, ok := cutBearer(header)
		if !ok {
			httpx.Fail(c, httpx.Unauthorized("未登录"))
			c.Abort()
			return
		}
		parsed, err := t.Parse(raw)
		if err != nil {
			httpx.Fail(c, err)
			c.Abort()
			return
		}
		c.Set("userID", parsed.Subject)
		c.Set("username", parsed.Username)
		c.Set("role", parsed.Role)
		c.Next()
	}
}

// RequireWriter 挡在所有会改数据的路由前面。viewer 只能看，不能建节点、改站点或发布。
func RequireWriter() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetString("role") == model.RoleViewer {
			httpx.Fail(c, httpx.Forbidden("只读账号不能修改"))
			c.Abort()
			return
		}
		c.Next()
	}
}

// cutBearer 从 "Bearer <token>" 里取出令牌本身。
func cutBearer(header string) (string, bool) {
	// prefix 是 HTTP 规范里的令牌前缀，后面才是 JWT 本身。
	const prefix = "Bearer "
	if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
		return "", false
	}
	return header[len(prefix):], true
}

// Service 处理登录和首次管理员。
type Service struct {
	db     *gorm.DB
	tokens *Tokens
}

// NewService 把数据库和令牌签发器装到一起。
func NewService(db *gorm.DB, tokens *Tokens) *Service {
	return &Service{db: db, tokens: tokens}
}

// EnsureAdmin 只在用户表是空的时候创建管理员。已经有用户就不会改密码，所以改环境变量不会重置 admin。
func (s *Service) EnsureAdmin(username, password string) error {
	var count int64
	if err := s.db.Model(&model.User{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.db.Create(&model.User{
		ID:           model.NewID(),
		Username:     username,
		PasswordHash: string(hash),
		Role:         model.RoleAdmin,
	}).Error
}

// Login 核对用户名和 bcrypt 密码。用户不存在和密码错误返回同一句话，避免被人探出账号是否存在。
func (s *Service) Login(ctx context.Context, username, password string) (LoginResult, error) {
	var user model.User
	err := s.db.WithContext(ctx).Where("username = ?", username).First(&user).Error
	if err != nil {
		if err = store.NotFound(err, ""); err != nil && !isNotFound(err) {
			return LoginResult{}, err
		}
		return LoginResult{}, httpx.Unauthorized("用户名或密码错误")
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return LoginResult{}, httpx.Unauthorized("用户名或密码错误")
	}
	token, err := s.tokens.Issue(user)
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Token: token, User: toUser(user)}, nil
}

// ChangePassword 修改当前登录用户的密码。改不改由用户自己决定。
func (s *Service) ChangePassword(ctx context.Context, id, current, next string) (User, error) {
	if len(next) < 8 || len(next) > 72 {
		return User{}, httpx.Invalid("新密码需要 8 到 72 位")
	}
	var user model.User
	if err := s.db.WithContext(ctx).First(&user, "id = ?", id).Error; err != nil {
		if err = store.NotFound(err, "用户不存在"); err != nil {
			return User{}, err
		}
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(current)) != nil {
		return User{}, httpx.Invalid("当前密码不正确")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	if err := s.db.WithContext(ctx).Model(&user).Update("password_hash", string(hash)).Error; err != nil {
		return User{}, err
	}
	user.PasswordHash = string(hash)
	return toUser(user), nil
}

// Me 用令牌里的用户编号再查一次库，确认这个账号还在。
func (s *Service) Me(ctx context.Context, id string) (User, error) {
	var user model.User
	if err := s.db.WithContext(ctx).First(&user, "id = ?", id).Error; err != nil {
		if err = store.NotFound(err, "用户不存在"); err != nil {
			return User{}, err
		}
	}
	return toUser(user), nil
}

// toUser 丢掉密码哈希，只留前端需要的三个字段。
func toUser(user model.User) User {
	return User{ID: user.ID, Username: user.Username, Role: user.Role}
}

// isNotFound 用来把“没有这个用户”收成统一的登录失败，而不是 404。
func isNotFound(err error) bool {
	_, ok := err.(*httpx.Error)
	return ok
}
