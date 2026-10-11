// Package sshclient 是控制面连到负载均衡节点的唯一出口。安装、探测、发布、回滚都通过它执行脚本。
// 第一次连接记住主机公钥，以后指纹变了就拒绝，避免连到另一台机器还把配置写过去。
package sshclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Spec 描述连哪台机器。密码和私钥二选一，KnownHostKey 为空表示这是第一次连接。
type Spec struct {
	Address      string
	Port         int
	User         string
	Password     string
	PrivateKey   []byte
	KnownHostKey string
	Timeout      time.Duration
}

// Result 带回命令输出。HostKey 是这次看到的主机公钥，探测成功后会写回节点表。
type Result struct {
	Stdout  string
	Stderr  string
	HostKey string
}

// Runner 把真正的 SSH 收成一个方法。测试可以换一个假实现，不用真的连机器。
type Runner interface {
	Run(ctx context.Context, spec Spec, script string, root bool) (Result, error)
}

// Remote 是生产用的 SSH 实现。它没有字段，连接信息每次由 Spec 传入。
type Remote struct{}

// Run 连上节点并执行脚本。root 为 true 时，已经是 root 就直接用 bash，否则用 sudo -n，不会交互式问密码。
// 脚本从标准输入送进去，不写到命令行上，避免节点的进程列表看到证书和密码。
func (Remote) Run(ctx context.Context, spec Spec, script string, root bool) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	client, hostKey, err := dial(ctx, spec)
	res := Result{HostKey: hostKey}
	if err != nil {
		return res, err
	}
	defer client.Close()

	shell := "sh -s"
	if root {
		uid, err := exec(ctx, client, "id -u", "")
		if err != nil {
			res.Stdout = uid.Stdout
			res.Stderr = uid.Stderr
			return res, err
		}
		if strings.TrimSpace(uid.Stdout) == "0" {
			shell = "bash -s"
		} else {
			shell = "sudo -n bash -s"
		}
	}
	out, err := exec(ctx, client, shell, script)
	res.Stdout = out.Stdout
	res.Stderr = out.Stderr
	return res, err
}

// Fingerprint 把保存的主机公钥变成 SHA256 指纹，界面上展示用。解析失败就返回空字符串。
func Fingerprint(authorized string) string {
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(authorized))
	if err != nil {
		return ""
	}
	return ssh.FingerprintSHA256(pub)
}

// dial 建立 TCP 再升级成 SSH。超时和取消都走 ctx，发布任务到点后不会一直占着连接。
func dial(ctx context.Context, spec Spec) (*ssh.Client, string, error) {
	if spec.User == "" {
		return nil, "", errors.New("缺少 SSH 用户")
	}
	var auth []ssh.AuthMethod
	switch {
	case len(spec.PrivateKey) > 0:
		signer, err := ssh.ParsePrivateKey(spec.PrivateKey)
		if err != nil {
			return nil, "", fmt.Errorf("私钥无法解析: %w", err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	case spec.Password != "":
		auth = append(auth, ssh.Password(spec.Password))
	default:
		return nil, "", errors.New("缺少 SSH 密码或私钥")
	}

	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	var seen string
	cfg := &ssh.ClientConfig{
		User:            spec.User,
		Auth:            auth,
		HostKeyCallback: hostKeyCallback(spec.KnownHostKey, &seen),
		Timeout:         timeout,
	}
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(spec.Address, strconv.Itoa(spec.Port)))
	if err != nil {
		return nil, "", err
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, spec.Address, cfg)
	if err != nil {
		conn.Close()
		return nil, seen, err
	}
	return ssh.NewClient(c, chans, reqs), seen, nil
}

// hostKeyCallback 实现“第一次信任，之后比对”。known 为空就接受并把公钥记到 seen；不为空就比指纹。
func hostKeyCallback(known string, seen *string) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
		*seen = line
		if strings.TrimSpace(known) == "" {
			return nil
		}
		stored, _, _, _, err := ssh.ParseAuthorizedKey([]byte(known))
		if err != nil {
			return errors.New("已保存的主机密钥无法解析")
		}
		if ssh.FingerprintSHA256(stored) != ssh.FingerprintSHA256(key) {
			return errors.New("主机密钥与第一次连接不一致")
		}
		return nil
	}
}

// limitedBuffer 截断过长的命令输出，避免一个失控脚本把内存写满。
type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
}

// Write 在达到上限后仍返回成功，这样远端命令不会因为我们不读了而收到 SIGPIPE 以外的奇怪错误。
func (w *limitedBuffer) Write(p []byte) (int, error) {
	remain := w.limit - w.buf.Len()
	if remain <= 0 {
		return len(p), nil
	}
	if len(p) > remain {
		p = p[:remain]
	}
	_, _ = w.buf.Write(p)
	return len(p), nil
}

// exec 开一个 SSH 会话跑一条命令。标准输出最多留 256KiB，标准错误最多 64KiB。
// 远端非 0 退出时，把 stderr 收成 error，调用方可以写进任务日志。
func exec(ctx context.Context, client *ssh.Client, command, stdin string) (Result, error) {
	session, err := client.NewSession()
	if err != nil {
		return Result{}, err
	}
	defer session.Close()

	var stdout, stderr limitedBuffer
	stdout.limit = 256 << 10
	stderr.limit = 64 << 10
	session.Stdout = &stdout
	session.Stderr = &stderr
	if stdin != "" {
		session.Stdin = strings.NewReader(stdin)
	}

	done := make(chan error, 1)
	go func() { done <- session.Run(command) }()

	var runErr error
	select {
	case <-ctx.Done():
		_ = session.Close()
		runErr = ctx.Err()
	case runErr = <-done:
	}
	res := Result{Stdout: stdout.buf.String(), Stderr: stderr.buf.String()}
	if runErr == nil {
		return res, nil
	}
	var exit *ssh.ExitError
	if errors.As(runErr, &exit) {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(res.Stdout)
		}
		if msg == "" {
			msg = runErr.Error()
		}
		return res, fmt.Errorf("命令退出 %d: %s", exit.ExitStatus(), msg)
	}
	return res, runErr
}
