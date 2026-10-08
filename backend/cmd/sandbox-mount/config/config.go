// Package sandboxMountConfig holds the configuration for the sandbox-mount node agent.
package sandboxMountConfig

import (
	"time"

	"github.com/clear-route/orbit/internal/platform/config"
)

// Config holds the node agent's settings, loaded from cmd/sandbox-mount/config/{base,env}.yaml.
type Config struct {
	App struct {
		Version string `yaml:"version"`
	} `yaml:"app"`

	Node struct {
		KubeletDir string `yaml:"kubeletDir"`
		ResyncSecs int    `yaml:"resyncSecs"`
		RetrySecs  int    `yaml:"retrySecs"`
	} `yaml:"node"`

	Server struct {
		ExportRoot  string `yaml:"exportRoot"`
		ExportsFile string `yaml:"exportsFile"`
		Threads     int    `yaml:"threads"`
		GraceSecs   int    `yaml:"graceSecs"`
	} `yaml:"server"`

	Client struct {
		MountOptions string `yaml:"mountOptions"`
	} `yaml:"client"`

	Observability struct {
		ServiceName string `yaml:"serviceName"`
	} `yaml:"observability"`
}

// Resync is how often the agent reconciles without a pod event.
func (c *Config) Resync() time.Duration { return time.Duration(c.Node.ResyncSecs) * time.Second }

// Retry is how soon the agent reconciles again after a failure.
func (c *Config) Retry() time.Duration { return time.Duration(c.Node.RetrySecs) * time.Second }

// Current holds the node agent's configuration.
var Current = config.MustLoadService[Config]("cmd/sandbox-mount", config.Environment())
