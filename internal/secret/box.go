// Package secret 在写入数据库前加密敏感内容：SSH 密码、SSH 私钥、证书私钥，以及发布快照。
// 用的是 AES-GCM。密钥来自 data/data.key，丢了这个文件就解不开已有数据。
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
)

// Box 是一把已经准备好的加密器。整个进程共用一个。
type Box struct {
	gcm cipher.AEAD
}

// New 用 32 字节密钥创建加密器。密钥长度不对时会直接失败，避免用错密钥还继续启动。
func New(key []byte) (*Box, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{gcm: gcm}, nil
}

// Encrypt 加密一段明文。每次随机一个 nonce，所以同一段明文每次密文都不同。结果是 base64，方便存进文本列。
func (b *Box) Encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	nonce := make([]byte, b.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := b.gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

// Decrypt 解回明文。密文被改过、或换了 data.key，都会失败。
func (b *Box) Decrypt(blob string) (string, error) {
	if blob == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		return "", err
	}
	if len(raw) < b.gcm.NonceSize() {
		return "", errors.New("ciphertext is too short")
	}
	nonce, ct := raw[:b.gcm.NonceSize()], raw[b.gcm.NonceSize():]
	plain, err := b.gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
