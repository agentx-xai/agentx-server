package main

import (
	"fmt"
	"os"

	"agentx/server/internal/deploycheck"
)

func main() {
	if err := deploycheck.Check(os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("production manifest preflight passed")
}
