package main

import (
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"os"
	"strconv"
)

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
