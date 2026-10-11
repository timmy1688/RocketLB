// 命令 rocketlb 是控制面进程。开发环境由 scripts/service.sh 启动它，生产环境由 docker compose 启动同一个程序。
//
// 程序怎么跑起来：
//  1. main 调用 config.Load，从环境变量和 data/ 目录读出监听地址、数据库、管理员和密钥。
//  2. app.New 打开数据库、补齐表结构、准备登录和各项业务，并把 HTTP 路由挂好。
//  3. application.Run 开始在 :8080 上接收浏览器和接口请求，直到收到停止信号后退出。
//
// 控制面自己不转发流量。它把期望状态存在数据库里，需要发布时再通过 SSH 写到 nginx 和 keepalived 所在的节点。
package main

import (
	"log/slog"
	"os"

	"rocketlb/internal/app"
	"rocketlb/internal/config"
)

// main 是 Go 程序唯一的入口。进程启动后从这里开始，返回或 os.Exit 后进程结束。
func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "err", err)
		os.Exit(1)
	}
	application, err := app.New(cfg)
	if err != nil {
		slog.Error("start", "err", err)
		os.Exit(1)
	}
	if err := application.Run(); err != nil {
		slog.Error("serve", "err", err)
		os.Exit(1)
	}
}
