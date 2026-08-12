package main

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
)

var (
	DEFAULT_FORWARDING_TIMEOUT = time.Second * 5
)

func GetMoguraDir() (string, error) {
	return ResolveUserHome("~/.mogura")
}

func GetDefaultConfigPath() (string, error) {
	dir, err := GetMoguraDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, "config.yml"), nil
}

type Config struct {
	Bastion SSHConfig      `yaml:"bastion_ssh_config"`
	Tunnels []TunnelConfig `yaml:"tunnels"`
}

type SSHConfig struct {
	Name      string `yaml:"name"`
	Host      string `yaml:"host"`
	Port      int    `yaml:"port"`
	User      string `yaml:"user"`
	KeyPath   string `yaml:"key_path"`
	RemoteDNS string `yaml:"remote_dns"`

	KnownHostsPath string `yaml:"known_hosts_path"`
	// InsecureIgnoreHostKey disables bastion host key verification.
	// it makes the connection vulnerable to man-in-the-middle attacks.
	InsecureIgnoreHostKey bool `yaml:"insecure_ignore_host_key"`
}

type TunnelConfig struct {
	Name          string `yaml:"name"`
	LocalBindPort int    `yaml:"local_bind_port"`
	TargetType    string `yaml:"target_type"`
	Target        string `yaml:"target"`
	TargetPort    int    `yaml:"target_port"`

	ForwardingTimeout string `yaml:"forwarding_timeout"`
}

func LoadConfig(path string) (*Config, error) {
	c := &Config{}
	err := LoadFromYamlFile(path, c)
	if err != nil {
		return nil, err
	}

	return c, nil
}

func LoadFromYamlFile(filePath string, p any) error {
	yml, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	return yaml.Unmarshal(yml, p)
}

func hostport(host string, port int) string {
	return host + ":" + strconv.Itoa(port)
}
func localport(port int) string {
	return "localhost:" + strconv.Itoa(port)
}

func ResolveUserHome(path string) (string, error) {
	if i := strings.Index(path, "~/"); i == 0 {
		user, err := user.Current()
		if err != nil {
			return path, fmt.Errorf("can not resolved home dir: %v", err)
		}

		resolvedPath := user.HomeDir + string(os.PathSeparator) + path[2:]
		return resolvedPath, nil
	} else {
		return path, nil
	}
}
