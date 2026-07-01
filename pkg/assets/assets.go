package assets

import "embed"

//go:embed web
var WebAssets embed.FS

//go:embed rdp-agent.exe
var RDPAgentExe []byte

//go:embed default_config.yaml
var DefaultConfig []byte

//go:embed default_scenarios.json
var DefaultScenarios []byte

//go:embed default_templates.json
var DefaultTemplates []byte
