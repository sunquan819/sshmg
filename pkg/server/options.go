package server

import (
	"embed"
	"net"
)

type Options struct {
	Port     int    // 0 = use config file port
	Password string // empty = use config file password
	ConfigPath string // default "config.yaml"
	Desktop  bool   // true = desktop mode (suppress console logs, enable RDP auto-launch)

	Listener net.Listener // pre-created listener for port-race safety (desktop mode)

	WebAssets        embed.FS
	RDPAgentExe      []byte
	DefaultConfig    []byte
	DefaultScenarios []byte
	DefaultTemplates []byte
}
