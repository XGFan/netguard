package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/XGFan/go-utils"
	"gopkg.in/yaml.v3"
	"log"
	"net/http"
	"netguard"
)

func main() {
	go func() {
		fmt.Println(http.ListenAndServe(":6060", nil))
	}()
	file := flag.String("c", "config.yaml", "config location")
	flag.Parse()
	log.Println("Network Guard")
	open, err := utils.LocateAndRead(*file)
	if err != nil {
		log.Fatalf("read config error: %s", err)
	}
	checkers := new([]netguard.CheckerConf)
	err = yaml.Unmarshal(open, checkers)
	if err != nil {
		log.Fatalf("parse config error: %s", err)
	}
	ctx := context.Background()
	for _, c := range *checkers {
		go func(checker netguard.Checker) {
			checker.Check(ctx)
		}(netguard.AssembleChecker(c))
	}
	select {}
}
