package main

import (
	"os"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "validate" {
		// Remove "validate" from args so flag parsing works correctly
		os.Args = append(os.Args[:1], os.Args[2:]...)
		validate()
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "validate-rec" {
		// Remove "validate-rec" from args so flag parsing works correctly
		os.Args = append(os.Args[:1], os.Args[2:]...)
		validateRecursive()
		return
	}

	generate()
}
