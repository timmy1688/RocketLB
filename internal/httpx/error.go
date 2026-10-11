package httpx

import "fmt"

// Error 是已经决定好 HTTP 状态和给用户看的中文说明的错误。
// 业务代码返回它，Fail 负责写成响应。普通 error 则会被当成内部错误，详细原因只写日志。
type Error struct {
	Status  int
	Code    string
	Message string
}

// Error 让 *Error 可以被当成普通 error 传递。
func (e *Error) Error() string { return e.Message }

// Invalid 表示请求内容不合法，HTTP 400。
func Invalid(message string) *Error {
	return &Error{Status: 400, Code: "invalid", Message: message}
}

// Invalidf 用格式化字符串构造 400 错误。
func Invalidf(format string, args ...any) *Error {
	return Invalid(fmt.Sprintf(format, args...))
}

// Unauthorized 表示没登录或令牌失效，HTTP 401。前端收到后会回到登录页。
func Unauthorized(message string) *Error {
	return &Error{Status: 401, Code: "unauthorized", Message: message}
}

// Forbidden 表示已登录但角色不允许，例如只读账号尝试修改，HTTP 403。
func Forbidden(message string) *Error {
	return &Error{Status: 403, Code: "forbidden", Message: message}
}

// NotFound 表示要操作的记录不存在，HTTP 404。
func NotFound(message string) *Error {
	return &Error{Status: 404, Code: "not_found", Message: message}
}

// Conflict 表示和现有数据冲突，例如名称重复或节点还在集群里，HTTP 409。
func Conflict(message string) *Error {
	return &Error{Status: 409, Code: "conflict", Message: message}
}

// Conflictf 用格式化字符串构造 409 错误。
func Conflictf(format string, args ...any) *Error {
	return Conflict(fmt.Sprintf(format, args...))
}

// Unavailable 表示依赖的节点或 SSH 暂时不可用，HTTP 502。
func Unavailable(message string) *Error {
	return &Error{Status: 502, Code: "unavailable", Message: message}
}
