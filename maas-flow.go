package main

import (
	"fmt"

	"github.com/juju/gomaasapi"
)

func main() {
	fmt.Println("Hello, World")
	_ = gomaasapi.NodeStatusAllocated
}
