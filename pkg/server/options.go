package server

import "embed"

type Options struct {
	Port     int    // 0 = use config file port
	Password string // empty = use config file password
	ConfigPath string // default "config.yaml"
	Desktop  bool   // true = desktop mode (suppress console logs, enable RDP auto-launch)

	WebAssets        embed.FS
	RDPAgentExe      []byte
	DefaultConfig    []byte
	DefaultScenarios []byte
	DefaultTemplates []byte
}
