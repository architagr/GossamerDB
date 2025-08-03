package config

type MTLSInfo struct {
	Enabled  bool   `json:"enabled" yaml:"enabled"`   // Enable or disable mTLS
	CertFile string `json:"certFile" yaml:"certFile"` // Path to the mTLS certificate file
	KeyFile  string `json:"keyFile" yaml:"keyFile"`   // Path to the mTLS key file
	CACert   string `json:"caCert" yaml:"caCert"`     // Path to the CA certificate file for mTLS
}
type SecurityInfo struct {
	MTLS MTLSInfo `json:"mtls" yaml:"mtls"` // mTLS configuration for secure communication
}
