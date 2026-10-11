// Package config 读取进程配置。环境变量优先，没设置时用默认值。
// 开发脚本不传环境变量时，数据库指向本机 127.0.0.1:5432；生产容器会显式传入 postgres 主机名。
// JWT 密钥和数据加密密钥如果环境里没有，就写到 data/jwt.secret 和 data/data.key，权限 0600，下次启动继续用。
package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config 是进程启动后一直带着的设置。后面的业务包只读它，不再自己读环境变量。
// DataKey 加密 SSH 密码和证书私钥，JWTSecret 签发登录令牌。
type Config struct {
	Env            string        // debug 时 Gin 会打印路由；生产用 release
	HTTPAddr       string        // 控制面监听地址，默认 :8080
	DataDir        string        // 密钥和运行时文件目录，默认 data
	DBDriver       string        // postgres 或 sqlite
	DBDSN          string        // 数据库连接串
	AdminUser      string        // 只在用户表为空时用来创建第一个管理员
	AdminPassword  string        // 同上，已有用户时不会改密码
	JWTSecret      string        // 签发登录令牌的密钥
	DataKey        []byte        // 加密 SSH 密码和证书私钥的 32 字节密钥
	WebDist        string        // 前端构建目录，存在时由本进程托管
	SSHDialTimeout time.Duration // 单次 SSH 连接超时
}

// Load 组装配置。数据库默认连本机 Postgres；测试会显式改成 SQLite。
// 没提供 JWT 密钥或数据密钥时，第一次启动会生成并写到 data/，之后一直复用，否则旧数据无法解密。
func Load() (Config, error) {
	dataDir := env("ROCKETLB_DATA_DIR", "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return Config{}, fmt.Errorf("create data dir: %w", err)
	}

	jwt := os.Getenv("ROCKETLB_JWT_SECRET")
	if jwt == "" {
		var err error
		jwt, err = readOrCreate(filepath.Join(dataDir, "jwt.secret"), func() (string, error) {
			buf := make([]byte, 32)
			if _, err := rand.Read(buf); err != nil {
				return "", err
			}
			return hex.EncodeToString(buf), nil
		})
		if err != nil {
			return Config{}, err
		}
	}

	var key []byte
	if raw := os.Getenv("ROCKETLB_DATA_KEY"); raw != "" {
		decoded, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return Config{}, fmt.Errorf("ROCKETLB_DATA_KEY must be base64")
		}
		key = decoded
	} else {
		stored, err := readOrCreate(filepath.Join(dataDir, "data.key"), func() (string, error) {
			buf := make([]byte, 32)
			if _, err := rand.Read(buf); err != nil {
				return "", err
			}
			return base64.StdEncoding.EncodeToString(buf), nil
		})
		if err != nil {
			return Config{}, err
		}
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(stored))
		if err != nil {
			return Config{}, fmt.Errorf("data.key is not base64: %w", err)
		}
		key = decoded
	}

	driver := env("ROCKETLB_DB_DRIVER", "postgres")
	dsn := os.Getenv("ROCKETLB_DB_DSN")
	if dsn == "" {
		switch driver {
		case "sqlite":
			dsn = filepath.Join(dataDir, "rocketlb.db")
		case "postgres":
			dsn = "postgres://rocketlb:rocketlb@127.0.0.1:5432/rocketlb?sslmode=disable"
		}
	}

	web := env("ROCKETLB_WEB_DIST", "web/dist")
	if info, err := os.Stat(web); err != nil || !info.IsDir() {
		web = ""
	}

	cfg := Config{
		Env:            env("ROCKETLB_ENV", "production"),
		HTTPAddr:       env("ROCKETLB_HTTP_ADDR", ":8080"),
		DataDir:        dataDir,
		DBDriver:       driver,
		DBDSN:          dsn,
		AdminUser:      env("ROCKETLB_ADMIN_USER", "admin"),
		AdminPassword:  env("ROCKETLB_ADMIN_PASSWORD", "admin"),
		JWTSecret:      jwt,
		DataKey:        key,
		WebDist:        web,
		SSHDialTimeout: 10 * time.Second,
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate 在真正打开数据库之前拦住明显错误的配置。
func (c Config) Validate() error {
	switch c.DBDriver {
	case "sqlite", "postgres":
	default:
		return fmt.Errorf("ROCKETLB_DB_DRIVER must be sqlite or postgres")
	}
	if c.DBDSN == "" {
		return fmt.Errorf("database DSN is empty")
	}
	if c.AdminUser == "" || c.AdminPassword == "" {
		return fmt.Errorf("admin username and password are required")
	}
	if len(c.JWTSecret) < 16 {
		return fmt.Errorf("JWT secret must be at least 16 characters")
	}
	if len(c.DataKey) != 32 {
		return fmt.Errorf("data key must be 32 bytes")
	}
	if c.SSHDialTimeout <= 0 {
		return fmt.Errorf("ssh dial timeout must be positive")
	}
	return nil
}

// env 读取环境变量。变量不存在或是空字符串时用 fallback。
func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// readOrCreate 读取已有文件。文件不存在时调用 gen 生成内容，并以仅所有者可读写的权限保存。
func readOrCreate(path string, gen func() (string, error)) (string, error) {
	existing, err := os.ReadFile(path)
	if err == nil {
		return strings.TrimSpace(string(existing)), nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	value, err := gen()
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
		return "", err
	}
	return value, nil
}
