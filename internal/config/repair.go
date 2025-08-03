package config

type RepairInfo struct {
	Enabled                      bool `json:"enabled" yaml:"enabled"`                                           // Enable or disable repair operations
	AntiEntropyIntervalInSeconds int  `json:"antiEntropyIntervalInSeconds" yaml:"antiEntropyIntervalInSeconds"` // Interval for anti-entropy operations in seconds
}
