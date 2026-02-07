package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	get "github.com/hashicorp/go-getter"
)

func main() {
	var (
		base     = flag.String("base", "https://github.com/PrismarineJS/minecraft-data.git", "base url")
		platform = flag.String("platform", "pc", "platform of schemas")
		ver      = flag.String("version", "1.21.8", "version of schemas")
		out      = flag.String("o", "./scheme", "output dir path")
	)
	flag.Parse()

	if *out == "" {
		panic("output dir path required")
	}

	if *platform == "" {
		panic("platform url required")
	}

	if *ver == "" {
		panic("version required")
	}

	path := fmt.Sprintf("%s/%s-%s", *out, *platform, *ver)

	if err := os.RemoveAll(path); err != nil {
		panic(err)
	}

	log.Default().Printf("start downloading schemes %s", path)

	// https://github.com/PrismarineJS/minecraft-data/tree/master/data/pc/1.21.8
	url := fmt.Sprintf("git::%s//data/%s/%s", *base, *platform, *ver)

	// url = "git::https://github.com/hashicorp/go-getter.git//testdata"
	if err := get.Get(path, url); err != nil {
		panic(err)
	}

	log.Default().Printf("done downloading schemes %s", path)
}
