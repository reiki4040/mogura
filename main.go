package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"

	"github.com/reiki4040/mogura/mogura"
)

const (
	USAGE = `mogura - ssh tunneling tool.

usage:
  
  mogura [-config config.yml] 

options:
  -v: show version, hash, go version.
  -h: show this usage.

  -config: specified tunnel configuration file. default ~/.mogura/config.yml
`

	ENV_HOME = "HOME"
)

var (
	// for version info
	version   string
	hash      string
	goversion string

	showVer           bool
	showUsage         bool
	optConfigFilePath string
)

func init() {
	flag.BoolVar(&showUsage, "h", false, "show usage.")
	flag.BoolVar(&showVer, "v", false, "show version")
	flag.StringVar(&optConfigFilePath, "config", "", "config file path. default: ~/.mogura/config.yml")

	flag.Parse()
}

func usage() {
	fmt.Printf("%s\n", USAGE)
}

func showVersion() {
	fmt.Printf("%s (%s) %s\n", version, hash, goversion)
}

func main() {
	if showUsage {
		usage()
		os.Exit(0)
	}

	if showVer {
		showVersion()
		os.Exit(0)
	}

	// default or specified option
	confPath := GetDefaultConfigPath()
	if optConfigFilePath != "" {
		confPath = optConfigFilePath
	}

	c, err := LoadConfig(confPath)
	if err != nil {
		log.Fatalf("can not load config file %s: %v", confPath, err)
	}

	// default name is Bastion
	basName := c.Bastion.Name
	if basName == "" {
		basName = "Bastion"
	}

	if c.Bastion.Host == "" {
		log.Fatalf("bastion host is required.")
	}

	// default port 22(ssh default)
	basPort := c.Bastion.Port
	if basPort == 0 {
		basPort = 22
	}

	bastionHostPort := hostport(c.Bastion.Host, basPort)

	// default key path "~/.ssh/id_rsa"
	basKeyPath := c.Bastion.KeyPath
	if basKeyPath == "" {
		basKeyPath = "~/.ssh/id_rsa"
	}

	// resolved "~/"
	rKeyPath, err := ResolveUserHome(basKeyPath)
	if err != nil {
		log.Fatalf("can not resolved user home path in %s: %v", basKeyPath, err)
	}

	moguraMap := make(map[string]*mogura.Mogura, len(c.Tunnels))
	skipCount := 0
	portMap := make(map[int]struct{}, len(c.Tunnels))
	for i, t := range c.Tunnels {
		name := t.Name
		if t.Name == "" {
			name = fmt.Sprintf("no name settting %d", i+1)
		}

		if t.LocalBindPort == 0 {
			log.Printf("ERROR tunnel %s: missing local_bind_port, skip.", name)
			skipCount++
			continue
		}

		// duplicate port check
		_, exists := portMap[t.LocalBindPort]
		if exists {
			log.Printf("ERROR tunnel %s: duplicate local_bind_port %d, skip.", name, t.LocalBindPort)
			continue
		} else {
			portMap[t.LocalBindPort] = struct{}{}
		}

		localHostPort := localport(t.LocalBindPort)

		target := mogura.Target{
			TargetType: t.TargetType,
			Target:     t.Target,
			TargetPort: t.TargetPort,
		}
		err := target.Validate()
		if err != nil {
			log.Printf("ERROR tunnel %s: invalid tunnel target: %v, skip.", name, err)
			skipCount++
			continue
		}

		if t.TargetType == "SRV" {
			if c.Bastion.RemoteDNS == "" {
				log.Printf("ERROR tunnel %s: remote_dns is required when target type is SRV, skip.", name)
				skipCount++
				continue
			}
		}
		moguraConfig := mogura.MoguraConfig{
			Name:             basName + " -> " + name,
			BastionHostPort:  bastionHostPort,
			Username:         c.Bastion.User,
			KeyPath:          rKeyPath,
			LocalBindPort:    localHostPort,
			RemoteDNS:        c.Bastion.RemoteDNS,
			ForwardingTarget: target,
		}

		forwardingTarget := t.Target
		if t.TargetPort > 0 {
			forwardingTarget += ":" + strconv.Itoa(t.TargetPort)
		}
		log.Printf("starting tunnel %s", moguraConfig.Name)
		log.Printf("%s -> %s -> %s", localHostPort, bastionHostPort, forwardingTarget)
		mogura, err := mogura.GoMogura(moguraConfig)
		if err != nil {
			/*
				TODO retry and error handling with other connection closing.
				currently, user self stop and connection not close handling...
			*/
			log.Printf("start %s tunnel failed: %v", t.Name, err)

			continue
		}

		// show transfer error
		go func(t TunnelConfig) {
			for tErr := range mogura.ErrChan() {
				/*
				 TODO if too many got error then reconnection?
				 use mogura.ConnectSSH(), mogura.ResolveRemote(), mogura.Listen()
				*/
				log.Printf("%s tunnel transfer failed: %v", t.Name, tErr)
			}
		}(t)

		// set map for control
		moguraMap[t.Name] = mogura
		log.Printf("started tunnel %s", mogura.Config.Name)
	}

	// all tunnel is wrong
	if skipCount == len(c.Tunnels) {
		log.Fatalf("all tunnels are invalid. mogura was not started.")
	}

	if skipCount > 0 {
		log.Printf("some tunnels are invalid. those tunnel were not started.")
	}

	log.Printf("mogura is started. mogura stop with press Ctrl+C")

	// waiting Ctrl + C
	quit := make(chan os.Signal)
	signal.Notify(quit, os.Interrupt)
	<-quit
	log.Printf("stopping mogura because got signal...")
	for n, m := range moguraMap {
		m.Close()
		log.Printf("closed %s tunnel.", n)
	}
	log.Printf("stopped mogura.")
}
