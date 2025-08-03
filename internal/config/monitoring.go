package config

type MonitoringInfo struct {
	Enabled     bool   `json:"enabled" yaml:"enabled"`         // Enable or disable monitoring
	MinLogLevel string `json:"minLogLevel" yaml:"minLogLevel"` // Minimum log level for monitoring (e.g., "info", "debug", "error")
}
