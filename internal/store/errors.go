package store

import (
	"errors"
	"rocketlb/internal/httpx"
	"strings"

	"gorm.io/gorm"
)

// 这两个函数把数据库驱动的原始错误翻译成业务错误，handler 不用关心 SQLite 和 Postgres 的报错长什么样。

// NotFound 把“查无此行”换成 404。其他数据库错误原样返回，最后会被当成内部错误。
func NotFound(err error, message string) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return httpx.NotFound(message)
	}
	return err
}

// Duplicate 判断是不是唯一约束冲突，例如节点地址或集群名字已经存在。
func Duplicate(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate")
}
