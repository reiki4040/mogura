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
	ENV_HOME = "HOME"
)

var (
	// for version info
	version   string
	hash      string
	goversion string

	showVer           bool
	optConfigFilePath string
)

func init() {
	flag.BoolVar(&showVer, "v", false, "show version")

	defConf := GetDefaultConfigPath()
	flag.StringVar(&optConfigFilePath, "config", "", fmt.Sprintf("config file path. default: %s", defConf))

	flag.Parse()
}

func showVersion() {
	fmt.Printf("%s (%s) %s\n", version, hash, goversion)
}

func main() {
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
	for i, t := range c.Tunnels {

		localHostPort := localport(t.LocalBindPort)

		name := t.Name
		if t.Name == "" {
			name = fmt.Sprintf("no name settting %d", i+1)
		}
		moguraConfig := mogura.MoguraConfig{
			Name:            basName + " -> " + name,
			BastionHostPort: bastionHostPort,
			Username:        c.Bastion.User,
			KeyPath:         rKeyPath,
			LocalBindPort:   localHostPort,
			RemoteDNS:       c.Bastion.RemoteDNS,
			ForwardingTarget: mogura.Target{
				TargetType: t.TargetType,
				Target:     t.Target,
				TargetPort: t.TargetPort,
			},
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
		} else {
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
		}

		// set map for control
		moguraMap[t.Name] = mogura
		log.Printf("started tunnel %s", mogura.Config.Name)
	}

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
