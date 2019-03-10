package main

import (
	"fmt"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"os"
	"os/user"
	"strconv"
	"strings"
)

func GetMoguraDir() string {
	return os.Getenv(ENV_HOME) + string(os.PathSeparator) + ".mogura"
}

func GetDefaultConfigPath() string {
	return GetMoguraDir() + string(os.PathSeparator) + "config.yml"
}

type Config struct {
	Bastion SSHConfig      `bastion_ssh_config`
	Tunnels []TunnelConfig `yaml:"tunnels"`
}

type SSHConfig struct {
	Name      string `yaml:"name"`
	Host      string `yaml:"host"`
	Port      int    `yaml:"port"`
	User      string `yaml:"user"`
	KeyPath   string `yaml:"key_path"`
	RemoteDNS string `yaml:"remote_dns"`
}

type TunnelConfig struct {
	Name          string `yaml:"name"`
	LocalBindPort int    `yaml:"local_bind_port"`
	TargetType    string `yaml:"target_type"`
	Target        string `yaml:"target"`
	TargetPort    int    `yaml:"target_port"`
}

func LoadConfig(path string) (*Config, error) {
	c := &Config{}
	err := LoadFromYamlFile(path, c)
	if err != nil {
		return nil, err
	}

	return c, nil
}

func LoadFromYamlFile(filePath string, p interface{}) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	yml, err := ioutil.ReadFile(filePath)
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
