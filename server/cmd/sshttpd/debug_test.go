package main

import (
	"fmt"
	"io/fs"
)

func main() {
	fmt.Println("Walking StaticFS:")
	fs.WalkDir(StaticFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			fmt.Printf("Error at %s: %v\n", path, err)
			return nil
		}
		fmt.Println(path)
		return nil
	})
	
	fmt.Println("\n\nTrying to get static subfs and walk:")
	subFS, err := fs.Sub(StaticFS, "static")
	if err != nil {
		fmt.Printf("Error getting static subfs: %v\n", err)
		return
	}
	fs.WalkDir(subFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			fmt.Printf("Error at %s: %v\n", path, err)
			return nil
		}
		fmt.Println(path)
		return nil
	})
}
