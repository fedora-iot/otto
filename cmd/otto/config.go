package main

import (
	"io"
	"os"

	"github.com/BurntSushi/toml"
)

type OttoConfig struct {
	Root string `toml:"root"`

	Addr string `toml:"listen"`
	TLS  struct {
		Cert string `toml:"cert"`
		Key  string `toml:"key"`
	} `toml:"tls"`
}

func (cfg *OttoConfig) LoadConfig(path string) error {

	var new_cfg OttoConfig

	_, err := toml.DecodeFile(path, &new_cfg)

	if err != nil && !os.IsNotExist(err) {
		return err
	}

	if new_cfg.Addr != "" {
		cfg.Addr = new_cfg.Addr
	}

	if new_cfg.Root != "" {
		cfg.Root = new_cfg.Root
	}

	if new_cfg.TLS.Cert != "" {
		cfg.TLS.Cert = new_cfg.TLS.Cert
	}

	if new_cfg.TLS.Key != "" {
		cfg.TLS.Key = new_cfg.TLS.Key
	}

	return nil
}

func (c *OttoConfig) DumpConfig(w io.Writer) error {
	return toml.NewEncoder(w).Encode(c)
}
