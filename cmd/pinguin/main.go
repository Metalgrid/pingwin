package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/metalgrid/pingwin"
)

func main() {
	// parse some flags
	var (
		count         int
		size          int
		interval      int
		timeout       int
		packetTimeout int
		hosts         []string
	)

	appCtx, cancel := context.WithCancel(context.Background())

	defer cancel() // TODO: call this in a signal handler

	flag.IntVar(&count, "c", 4, "Packets to send per host.")
	flag.IntVar(&size, "s", 56, "Size of the packet to send, in bytes.")
	flag.IntVar(&interval, "i", 1000, "Wait interval seconds between sending each packet, per host.")
	flag.IntVar(&timeout, "t", 2000, "Time to wait for a response, in milliseconds.")
	flag.IntVar(&packetTimeout, "w", 1000, "Time to wait for a response, in milliseconds.")
	flag.Parse()
	hosts = flag.Args()

	// create a new pingwin
	pw := pingwin.NewPingwin(count, size, interval, timeout, packetTimeout)

	results := pw.Run(appCtx, hosts)

	for result := range results {
		// do something with the result
		fmt.Println(result)
	}

}
