package main

import (
	"log"
	"os"
	"os/signal"

	"github.com/reiki4040/mogura/mogura"
)

func main() {
	c := &Config{}
	err := LoadFromYamlFile("./config.yml", c)
	if err != nil {
		log.Fatalf("can not load config file: %v", err)
	}

	for _, t := range c.Tunnel {
		mogura := &mogura.Mogura{
			Name:                 c.Bastion.Name + " -> " + t.Name,
			BastionHostPort:      hostport(c.Bastion.Host, c.Bastion.Port),
			Username:             c.Bastion.User,
			KeyPath:              c.Bastion.KeyPath,
			LocalBindPort:        localport(t.LocalBindPort),
			ForwardingRemotePort: hostport(t.RemoteHost, t.RemotePort),
		}

		errChan, err := mogura.Go()
		if err != nil {
			/*
				TODO retry and error handling with other connection closing.
				currently, user self stop and connection not close handling...
			*/
			log.Printf("start %s tunnel failed: %v", t.Name, err)
		} else {
			// show transfer error
			go func() {
				for tErr := range errChan {
					// TODO if too many got error then reconnection?
					log.Printf("%s tunnel transfer failed: %v", t.Name, tErr)
				}
			}()
		}
	}

	// waiting Ctrl + C
	quit := make(chan os.Signal)
	signal.Notify(quit, os.Interrupt)
	<-quit
	log.Printf("stopped mogura because got signal.")
}
