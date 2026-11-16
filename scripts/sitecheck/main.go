package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "用法: sitecheck <官网目录>")
		os.Exit(2)
	}
	violations, err := Check(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if len(violations) > 0 {
		for _, v := range violations {
			fmt.Printf("%s:%d: %s\n", v.File, v.Line, v.Reason)
		}
		os.Exit(1)
	}
	fmt.Println("检查通过")
}
