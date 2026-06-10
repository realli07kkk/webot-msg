package main

import (
	"flag"
	"log"

	"github.com/realli07kkk/webot-msg/internal/app"
	"github.com/realli07kkk/webot-msg/internal/config"
	"github.com/realli07kkk/webot-msg/internal/ilink"
)

func main() {
	port := flag.Int("port", 26322, "API server port")
	flag.Parse()

	application := app.New(config.DefaultPath, ilink.DefaultBaseURL)
	if err := application.Run(*port); err != nil {
		log.Fatal(err)
	}
}
