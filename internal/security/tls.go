package security

import (
	"GossamerDB/internal/config"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
)

// MTLSConfig holds paths to cert files and Enable flag
type MTLSConfig struct {
	Enabled  bool
	CertFile string
	KeyFile  string
	CACert   string
}

var (
	tlsConfig *tls.Config
	once      sync.Once
)

// LoadMTLSConfig loads and validates mTLS config from MTLSConfig struct
// Returns nil tls.Config if mTLS disabled
func LoadMTLSConfig() (*tls.Config, error) {
	var err error
	once.Do(func() {
		cfg := config.ConfigObj.Security.MTLS
		if !cfg.Enabled {
			log.Print("[MTLS] mTLS is disabled")
			return
		}

		// Read cert and key files
		certPEMBlock, fileReadErr := os.ReadFile(cfg.CertFile)
		if fileReadErr != nil {
			err = fmt.Errorf("failed to read cert file %s: %w", cfg.CertFile, fileReadErr)
			return
		}
		keyPEMBlock, fileReadErr := os.ReadFile(cfg.KeyFile)
		if fileReadErr != nil {
			err = fmt.Errorf("failed to read key file %s: %w", cfg.KeyFile, fileReadErr)
			return
		}

		// Load X509 key pair
		cert, fileReadErr := tls.X509KeyPair(certPEMBlock, keyPEMBlock)
		if fileReadErr != nil {
			err = fmt.Errorf("failed to load X509 key pair: %w", fileReadErr)
			return
		}

		// Load CA cert pool
		caCertPool := x509.NewCertPool()
		if cfg.CACert != "" {
			caCertPEM, fileReadErr := os.ReadFile(cfg.CACert)
			if fileReadErr != nil {
				err = fmt.Errorf("failed to read CA cert file %s: %w", cfg.CACert, fileReadErr)
				return
			}
			if !caCertPool.AppendCertsFromPEM(caCertPEM) {
				err = fmt.Errorf("failed to append CA cert to pool")
				return
			}
		} else {
			log.Print("[MTLS] Warning: no CA cert provided, skipping client cert verification")
		}

		tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			ClientAuth:   tls.RequireAndVerifyClientCert,
			ClientCAs:    caCertPool,
			MinVersion:   tls.VersionTLS12,
			// You can customize other TLS config items here (CipherSuites, etc)
		}

		log.Print("[MTLS] mTLS config successfully loaded")
	})
	return tlsConfig, err
}

func ConfigureSecureServer(addr string, h http.Handler) (*http.Server, error) {
	tlsConf, err := LoadMTLSConfig()
	if err != nil {
		return nil, err
	}
	log.Printf("Configuring secure server on %s", addr)
	return &http.Server{
		Addr:      addr,
		TLSConfig: tlsConf,
		Handler:   h,
	}, nil
}

func NewMTLSHttpClient() (*http.Client, error) {
	tlsConf, err := LoadMTLSConfig()
	if err != nil {
		return nil, err
	}

	tlsConf.InsecureSkipVerify = false // Always verify!

	tr := &http.Transport{
		TLSClientConfig: tlsConf,
	}
	client := &http.Client{Transport: tr}
	return client, nil
}
