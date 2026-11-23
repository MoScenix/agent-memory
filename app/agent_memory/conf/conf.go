package conf

import (
	"embed"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

//go:embed dev/conf.yaml test/conf.yaml online/conf.yaml
var configs embed.FS

type Config struct {
	Hertz     Hertz     `yaml:"hertz"`
	MySQL     MySQL     `yaml:"mysql"`
	Milvus    Milvus    `yaml:"milvus"`
	Outbox    Outbox    `yaml:"outbox"`
	Embedding Embedding `yaml:"embedding"`
}

type Hertz struct {
	Address string `yaml:"address"`
}

type MySQL struct {
	Address  string `yaml:"address"`
	Database string `yaml:"database"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type Milvus struct {
	Address    string `yaml:"address"`
	Database   string `yaml:"database"`
	Collection string `yaml:"collection"`
	Token      string `yaml:"token"`
}

type Outbox struct {
	PollInterval   time.Duration `yaml:"poll_interval"`
	ProcessTimeout time.Duration `yaml:"process_timeout"`
	LeaseDuration  time.Duration `yaml:"lease_duration"`
	MaxAttempts    uint32        `yaml:"max_attempts"`
	RetryBase      time.Duration `yaml:"retry_base"`
	RetryMax       time.Duration `yaml:"retry_max"`
}

type Embedding struct {
	BaseURL    string `yaml:"base_url"`
	Model      string `yaml:"model"`
	Dimensions int    `yaml:"dimensions"`
	APIKey     string `yaml:"-"`
}

func Load(env string) (Config, error) {
	if env == "" {
		env = "dev"
	}
	switch env {
	case "dev", "test", "online":
	default:
		return Config{}, fmt.Errorf("unknown GO_ENV %q", env)
	}

	data, err := configs.ReadFile(env + "/conf.yaml")
	if err != nil {
		return Config{}, fmt.Errorf("read %s config: %w", env, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s config: %w", env, err)
	}
	if cfg.Hertz.Address == "" {
		return Config{}, fmt.Errorf("%s config is missing hertz.address", env)
	}

	overrides := []struct {
		name  string
		field *string
	}{
		{"AGENT_MEMORY_MYSQL_ADDRESS", &cfg.MySQL.Address},
		{"AGENT_MEMORY_MYSQL_DATABASE", &cfg.MySQL.Database},
		{"AGENT_MEMORY_MYSQL_USERNAME", &cfg.MySQL.Username},
		{"AGENT_MEMORY_MYSQL_PASSWORD", &cfg.MySQL.Password},
		{"AGENT_MEMORY_MILVUS_ADDRESS", &cfg.Milvus.Address},
		{"AGENT_MEMORY_MILVUS_DATABASE", &cfg.Milvus.Database},
		{"AGENT_MEMORY_MILVUS_COLLECTION", &cfg.Milvus.Collection},
		{"AGENT_MEMORY_MILVUS_TOKEN", &cfg.Milvus.Token},
		{"AGENT_MEMORY_EMBEDDING_BASE_URL", &cfg.Embedding.BaseURL},
		{"AGENT_MEMORY_EMBEDDING_MODEL", &cfg.Embedding.Model},
		{"AGENT_MEMORY_EMBEDDING_API_KEY", &cfg.Embedding.APIKey},
	}
	cfg.Embedding.APIKey = os.Getenv("DASHSCOPE_API_KEY")
	if _, hasBaseURL := os.LookupEnv("AGENT_MEMORY_EMBEDDING_BASE_URL"); !hasBaseURL {
		if endpoint := os.Getenv("AGENT_MEMORY_EMBEDDING_ENDPOINT"); endpoint != "" {
			cfg.Embedding.BaseURL = strings.TrimSuffix(strings.TrimRight(endpoint, "/"), "/embeddings")
		}
	}
	for _, override := range overrides {
		if value, ok := os.LookupEnv(override.name); ok {
			*override.field = value
		}
	}

	return cfg, nil
}
