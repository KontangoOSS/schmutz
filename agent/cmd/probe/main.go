package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/KontangoOSS/schmutz/agent/pkg/schmutz/discovery"
)

func main() {
	enableL4 := false
	for _, a := range os.Args[1:] {
		if a == "--l4" {
			enableL4 = true
		}
	}
	layers := discovery.LocalLayers()
	if enableL4 {
		layers = discovery.AllLayers()
	}
	res := discovery.Probe{Layers: layers}.Run(context.Background())
	out, _ := json.MarshalIndent(res, "", "  ")
	fmt.Println(string(out))
}
