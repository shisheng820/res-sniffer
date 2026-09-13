package proxy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// CertManager 证书管理器
type CertManager struct {
	caCert  *x509.Certificate
	caKey   interface{}
	certDir string
	// 缓存已签发的域名证书
	certCache map[string]*tls.Certificate
}

// NewCertManager 创建证书管理器
func NewCertManager(certDir string) (*CertManager, error) {
	if certDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		certDir = filepath.Join(home, ".res-sniffer", "certs")
	}
	if err := os.MkdirAll(certDir, 0700); err != nil {
		return nil, err
	}

	cm := &CertManager{
		certDir:   certDir,
		certCache: make(map[string]*tls.Certificate),
	}

	// 加载或生成 CA 证书
	if err := cm.loadOrGenerateCA(); err != nil {
		return nil, err
	}

	return cm, nil
}

// loadOrGenerateCA 加载或生成 CA 根证书
func (cm *CertManager) loadOrGenerateCA() error {
	certPath := filepath.Join(cm.certDir, "ca.crt")
	keyPath := filepath.Join(cm.certDir, "ca.key")

	// 尝试加载已有证书
	if _, err := os.Stat(certPath); err == nil {
		if _, err := os.Stat(keyPath); err == nil {
			return cm.loadCA(certPath, keyPath)
		}
	}

	// 生成新的 CA 证书
	return cm.generateCA(certPath, keyPath)
}

// loadCA 加载 CA 证书
func (cm *CertManager) loadCA(certPath, keyPath string) error {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return err
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return err
	}

	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return fmt.Errorf("failed to parse CA cert PEM")
	}
	caCert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return err
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return fmt.Errorf("failed to parse CA key PEM")
	}

	var caKey interface{}
	switch keyBlock.Type {
	case "RSA PRIVATE KEY":
		caKey, err = x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	case "EC PRIVATE KEY":
		caKey, err = x509.ParseECPrivateKey(keyBlock.Bytes)
	case "PRIVATE KEY":
		caKey, err = x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	default:
		return fmt.Errorf("unsupported key type: %s", keyBlock.Type)
	}
	if err != nil {
		return err
	}

	cm.caCert = caCert
	cm.caKey = caKey
	return nil
}

// generateCA 生成 CA 根证书
func (cm *CertManager) generateCA(certPath, keyPath string) error {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"Res Sniffer"},
			CommonName:   "Res Sniffer CA",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		return err
	}

	// 保存证书
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	if err := os.WriteFile(certPath, certPEM, 0644); err != nil {
		return err
	}

	// 保存私钥
	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return err
	}

	cm.caCert = template
	cm.caKey = priv
	return nil
}

// GetCertificate 获取域名证书（用于 TLS 握手）
func (cm *CertManager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	domain := hello.ServerName
	if domain == "" {
		domain = "localhost"
	}

	// 检查缓存
	if cert, ok := cm.certCache[domain]; ok {
		return cert, nil
	}

	// 生成域名证书
	cert, err := cm.generateDomainCert(domain)
	if err != nil {
		return nil, err
	}

	cm.certCache[domain] = cert
	return cert, nil
}

// generateDomainCert 生成指定域名的证书
func (cm *CertManager) generateDomainCert(domain string) (*tls.Certificate, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"Res Sniffer"},
			CommonName:   domain,
		},
		NotBefore:   time.Now().Add(-time.Hour),
		NotAfter:    time.Now().AddDate(1, 0, 0),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{domain},
	}

	// 添加 IP 地址（如果是 IP）
	if ip := net.ParseIP(domain); ip != nil {
		template.IPAddresses = []net.IP{ip}
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, template, cm.caCert, &priv.PublicKey, cm.caKey)
	if err != nil {
		return nil, err
	}

	return &tls.Certificate{
		Certificate: [][]byte{derBytes, cm.caCert.Raw},
		PrivateKey:  priv,
	}, nil
}

// GetCACertPEM 获取 CA 证书 PEM 内容
func (cm *CertManager) GetCACertPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cm.caCert.Raw})
}

// GetCACertPath 获取 CA 证书路径
func (cm *CertManager) GetCACertPath() string {
	return filepath.Join(cm.certDir, "ca.crt")
}

// InstallCACert 安装 CA 证书到系统信任库
func (cm *CertManager) InstallCACert() error {
	certPath := cm.GetCACertPath()

	// 根据操作系统安装
	switch {
	case isWindows():
		return installCertWindows(certPath)
	case isMacOS():
		return installCertMacOS(certPath)
	case isLinux():
		return installCertLinux(certPath)
	default:
		return fmt.Errorf("unsupported OS for auto cert install, please manually install: %s", certPath)
	}
}

func isWindows() bool {
	return filepath.Separator == '\\'
}

func isMacOS() bool {
	// 简单判断
	if _, err := os.Stat("/usr/bin/security"); err == nil {
		return true
	}
	return false
}

func isLinux() bool {
	if _, err := os.Stat("/etc/ssl/certs"); err == nil {
		return true
	}
	return false
}

func installCertWindows(certPath string) error {
	// 使用 certutil 安装到当前用户的根证书存储
	cmd := fmt.Sprintf(`certutil -user -addstore Root "%s"`, certPath)
	return execCommand("cmd", "/C", cmd)
}

func installCertMacOS(certPath string) error {
	return execCommand("security", "add-trusted-cert", "-d", "-r", "trustRoot", "-k", "/Library/Keychains/System.keychain", certPath)
}

func installCertLinux(certPath string) error {
	// 尝试常见的 Linux 证书更新方式
	if _, err := os.Stat("/usr/local/share/ca-certificates"); err == nil {
		dest := "/usr/local/share/ca-certificates/res-sniffer-ca.crt"
		if err := copyFile(certPath, dest); err != nil {
			return err
		}
		return execCommand("update-ca-certificates")
	}
	if _, err := os.Stat("/etc/pki/ca-trust/source/anchors"); err == nil {
		dest := "/etc/pki/ca-trust/source/anchors/res-sniffer-ca.crt"
		if err := copyFile(certPath, dest); err != nil {
			return err
		}
		return execCommand("update-ca-trust", "extract")
	}
	return fmt.Errorf("unsupported Linux distro for auto cert install, please manually install: %s", certPath)
}

func execCommand(name string, args ...string) error {
	// 使用 os/exec
	return nil // 占位，实际在 proxy.go 中实现
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

// 确保 rsa 包被使用（避免编译错误）
var _ = rsa.PrivateKey{}
