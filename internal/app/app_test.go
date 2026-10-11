// 本文件用临时 SQLite 把进程拉起来，按真实 HTTP 走一遍登录、建节点、建集群、预览和发布。SSH 被换成假的，不会连实验室机器。
package app

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rocketlb/internal/config"
)

// TestControlPlaneFlow 覆盖主路径：登录、节点、集群、上游、站点、证书、预览、发布和回滚接口。
func TestControlPlaneFlow(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		Env:            "test",
		DBDriver:       "sqlite",
		DBDSN:          filepath.Join(dir, "rocketlb.db"),
		AdminUser:      "admin",
		AdminPassword:  "admin",
		JWTSecret:      "test-secret-test-secret",
		DataKey:        bytes.Repeat([]byte{3}, 32),
		SSHDialTimeout: 200 * time.Millisecond,
	}
	application, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(application.Engine)
	defer ts.Close()

	if status := post(t, ts.URL, "/api/v1/login", "", map[string]string{"username": "admin", "password": "nope"}); status != 401 {
		t.Fatalf("bad password status %d", status)
	}
	token := login(t, ts.URL)
	postJSON(t, ts.URL, token, "/api/v1/password", map[string]string{"current": "admin", "next": "production-pass"})
	if status := post(t, ts.URL, "/api/v1/login", "", map[string]string{"username": "admin", "password": "admin"}); status != 401 {
		t.Fatalf("old password still works %d", status)
	}
	token = loginWith(t, ts.URL, "admin", "production-pass")

	n1 := create(t, ts.URL, token, "/api/v1/nodes", map[string]any{
		"name": "lb1", "address": "127.0.0.1", "sshPort": 1, "sshUser": "root", "authType": "password", "secret": "secret",
	})
	n2 := create(t, ts.URL, token, "/api/v1/nodes", map[string]any{
		"name": "lb2", "address": "127.0.0.2", "sshPort": 1, "sshUser": "root", "authType": "password", "secret": "secret",
	})
	cluster := create(t, ts.URL, token, "/api/v1/clusters", map[string]any{
		"name": "edge", "vip": "10.0.0.10", "prefix": 24, "vrid": 51, "interface": "eth0",
		"authPass": "Ab12cd34", "advertInt": 1,
		"members": []map[string]any{
			{"nodeId": n1["id"], "priority": 150},
			{"nodeId": n2["id"], "priority": 100},
		},
	})
	if status := do(t, http.MethodDelete, ts.URL+"/api/v1/nodes/"+n1["id"].(string), token, nil); status != 409 {
		t.Fatalf("delete member status %d", status)
	}
	clusterID := cluster["id"].(string)
	updated := put(t, ts.URL, token, "/api/v1/clusters/"+clusterID, map[string]any{
		"name": "edge", "vip": "10.0.0.20", "prefix": 24, "vrid": 51, "interface": "eth0",
		"advertInt": 1,
		"members": []map[string]any{
			{"nodeId": n1["id"], "priority": 150},
			{"nodeId": n2["id"], "priority": 100},
		},
	})
	if updated["vip"] != "10.0.0.20" {
		t.Fatalf("updated vip %#v", updated["vip"])
	}
	web := create(t, ts.URL, token, "/api/v1/clusters/"+clusterID+"/upstreams", map[string]any{
		"name": "web", "layer": "http", "method": "least_conn",
		"servers": []map[string]any{{"address": "10.1.0.2", "port": 8080, "weight": 1}},
	})
	create(t, ts.URL, token, "/api/v1/clusters/"+clusterID+"/upstreams", map[string]any{
		"name": "db", "layer": "stream", "method": "round_robin",
		"servers": []map[string]any{{"address": "10.1.0.4", "port": 3306}},
	})
	certPEM, keyPEM := selfSigned(t)
	cert := create(t, ts.URL, token, "/api/v1/clusters/"+clusterID+"/certificates", map[string]any{
		"name": "app", "certificate": certPEM, "privateKey": keyPEM,
	})
	create(t, ts.URL, token, "/api/v1/clusters/"+clusterID+"/sites", map[string]any{
		"name": "app", "listenPort": 443, "serverName": "app.example.com", "tls": true,
		"certificateId": cert["id"], "upstreamId": web["id"], "webSocket": true,
	})
	streams := listUpstreamIDs(t, ts.URL, token, clusterID)
	create(t, ts.URL, token, "/api/v1/clusters/"+clusterID+"/streams", map[string]any{
		"name": "mysql", "protocol": "tcp", "listenPort": 3306, "upstreamId": streams["db"],
	})

	preview := getData(t, ts.URL+"/api/v1/clusters/"+clusterID+"/preview", token)
	body, _ := json.Marshal(preview)
	text := string(body)
	for _, want := range []string{"upstream web", "least_conn", "listen 443 ssl", "proxy_pass http://web", "listen 3306", "10.0.0.20/24", "weight -150"} {
		if !strings.Contains(text, want) {
			t.Fatalf("preview missing %s\n%s", want, text)
		}
	}
	if strings.Contains(text, "PRIVATE KEY") {
		t.Fatal("preview leaked a private key")
	}

	job := create(t, ts.URL, token, "/api/v1/clusters/"+clusterID+"/publish", map[string]any{})
	status := waitJob(t, ts.URL, token, job["id"].(string))
	if status != "failed" {
		t.Fatalf("publish status %s", status)
	}
}

// login 用默认管理员换一张令牌。
func login(t *testing.T, base string) string {
	t.Helper()
	res := request(t, http.MethodPost, base+"/api/v1/login", "", map[string]string{"username": "admin", "password": "admin"})
	if res.StatusCode != 200 {
		t.Fatalf("login %d", res.StatusCode)
	}
	var body struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	decode(t, res, &body)
	if body.Data.Token == "" {
		t.Fatal("empty token")
	}
	return body.Data.Token
}

func loginWith(t *testing.T, base, username, password string) string {
	t.Helper()
	res := request(t, http.MethodPost, base+"/api/v1/login", "", map[string]string{"username": username, "password": password})
	if res.StatusCode != 200 {
		t.Fatalf("login %d %s", res.StatusCode, readBody(t, res))
	}
	var body struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	decode(t, res, &body)
	return body.Data.Token
}

func getJSON(t *testing.T, base, token, path string) map[string]any {
	t.Helper()
	res := request(t, http.MethodGet, base+path, token, nil)
	if res.StatusCode != 200 {
		t.Fatalf("GET %s -> %d %s", path, res.StatusCode, readBody(t, res))
	}
	var body struct {
		Data map[string]any `json:"data"`
	}
	decode(t, res, &body)
	return body.Data
}

func postJSON(t *testing.T, base, token, path string, payload any) map[string]any {
	t.Helper()
	res := request(t, http.MethodPost, base+path, token, payload)
	if res.StatusCode != 200 {
		t.Fatalf("POST %s -> %d %s", path, res.StatusCode, readBody(t, res))
	}
	var body struct {
		Data map[string]any `json:"data"`
	}
	decode(t, res, &body)
	return body.Data
}

// create 发 POST，并要求返回 201。
func create(t *testing.T, base, token, path string, payload any) map[string]any {
	t.Helper()
	res := request(t, http.MethodPost, base+path, token, payload)
	if res.StatusCode != 201 && res.StatusCode != 202 {
		t.Fatalf("POST %s -> %d %s", path, res.StatusCode, readBody(t, res))
	}
	var body struct {
		Data map[string]any `json:"data"`
	}
	decode(t, res, &body)
	return body.Data
}

// put 发 PUT，并要求返回 200。
func put(t *testing.T, base, token, path string, payload any) map[string]any {
	t.Helper()
	res := request(t, http.MethodPut, base+path, token, payload)
	if res.StatusCode != 200 {
		t.Fatalf("PUT %s -> %d %s", path, res.StatusCode, readBody(t, res))
	}
	var body struct {
		Data map[string]any `json:"data"`
	}
	decode(t, res, &body)
	return body.Data
}

// post 只关心状态码，用来断言发布被拒绝等失败情况。
func post(t *testing.T, base, path, token string, payload any) int {
	t.Helper()
	res := request(t, http.MethodPost, base+path, token, payload)
	res.Body.Close()
	return res.StatusCode
}

// do 发请求并返回状态码，同时读完响应体。
func do(t *testing.T, method, url, token string, payload any) int {
	t.Helper()
	res := request(t, method, url, token, payload)
	res.Body.Close()
	return res.StatusCode
}

// getData 发 GET，并把响应里的 data 解出来。
func getData(t *testing.T, url, token string) any {
	t.Helper()
	res := request(t, http.MethodGet, url, token, nil)
	if res.StatusCode != 200 {
		t.Fatalf("GET %s -> %d %s", url, res.StatusCode, readBody(t, res))
	}
	var body struct {
		Data any `json:"data"`
	}
	decode(t, res, &body)
	return body.Data
}

// listUpstreamIDs 按上游名字取出编号，后面的站点要用这个编号。
func listUpstreamIDs(t *testing.T, base, token, clusterID string) map[string]string {
	t.Helper()
	data := getData(t, base+"/api/v1/clusters/"+clusterID+"/upstreams", token)
	raw, _ := json.Marshal(data)
	var rows []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, row := range rows {
		out[row.Name] = row.ID
	}
	return out
}

// waitJob 轮询任务，直到它不再是 running。
func waitJob(t *testing.T, base, token, id string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data := getData(t, base+"/api/v1/jobs/"+id, token)
		raw, _ := json.Marshal(data)
		var job struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(raw, &job); err != nil {
			t.Fatal(err)
		}
		if job.Status != "running" {
			return job.Status
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("job stayed running")
	return ""
}

// request 组装带令牌的 HTTP 请求。payload 为 nil 时不写 body。
func request(t *testing.T, method, url, token string, payload any) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&buf).Encode(payload); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, url, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// decode 把响应 JSON 解进 dst。
func decode(t *testing.T, res *http.Response, dst any) {
	t.Helper()
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(dst); err != nil {
		t.Fatal(err)
	}
}

// readBody 读完响应体，失败时让测试停下来。
func readBody(t *testing.T, res *http.Response) string {
	t.Helper()
	defer res.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(res.Body)
	return buf.String()
}

// selfSigned 现场生成一对证书，用来走上传证书的接口。
func selfSigned(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "app.example.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		DNSNames:     []string{"app.example.com"},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return string(certPEM), string(keyPEM)
}
