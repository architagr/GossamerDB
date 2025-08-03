package config

type PersistenceInfo struct {
	Enabled bool   `json:"enabled" yaml:"enabled"` // Enable or disable persistence
	Backend string `json:"backend" yaml:"backend"` // Backend for persistence (e.g., "file", "database")
	Path    string `json:"path" yaml:"path"`       // Path for persistence storage
}
