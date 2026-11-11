package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"modelbridge/internal/control"
	"modelbridge/internal/update"
	"modelbridge/internal/version"
)

var applyTestFlags = func(*control.Options) error { return nil }

func main() {
	showVersion := flag.Bool("version", false, "打印版本号")
	dataDir := flag.String("data-dir", "", "数据目录")
	flag.Parse()

	if *showVersion {
		fmt.Println(version.Current())
		return
	}
	if *dataDir == "" {
		fmt.Fprintln(os.Stderr, "缺少 --data-dir")
		os.Exit(2)
	}

	opts := control.Options{
		DataDir:          *dataDir,
		Version:          version.Current(),
		UpdateAPIBase:    "https://api.github.com",
		UpdateRepository: update.Repository(),
	}
	if err := applyTestFlags(&opts); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := control.Serve(context.Background(), opts, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
