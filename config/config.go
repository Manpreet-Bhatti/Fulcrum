package config

import (
	"encoding/json"
	"os"
)

type BackendConfig struct {
	URL  string `json:"url"`
	Name string `json:"name"`
}

type Config struct {
	LBPort   int             `json:"lb_port"`
	Backends []BackendConfig `json:"backends"`
}

func LoadConfig(file string) (*Config, error) {
	f, err := os.Open(file)

	if err != nil {
		return nil, err
	}

	defer f.Close()

	var config Config
	decoder := json.NewDecoder(f)
	err = decoder.Decode(&config)

	return &config, err
}
