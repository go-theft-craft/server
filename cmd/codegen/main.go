package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/OCharnyshevich/minecraft-server/cmd/codegen/internal/generator"
)

func main() {
	schemeDir := flag.String("scheme", "", "path to the scheme directory (e.g. ./scheme/pc-1.8)")
	outDir := flag.String("out", "./internal/gamedata/versions", "output base directory for generated packages")
	pkg := flag.String("pkg", "", "package name override (default: derived from scheme dir name)")

	flag.Parse()

	if *schemeDir == "" {
		fmt.Fprintln(os.Stderr, "error: -scheme flag is required")
		flag.Usage()
		os.Exit(1)
	}

	dirName := filepath.Base(*schemeDir)
	version := dirName

	pkgName := *pkg
	if pkgName == "" {
		pkgName = sanitizePackageName(dirName)
	}

	fmt.Printf("codegen: generating %s from %s\n", pkgName, *schemeDir)

	cfg := generator.Config{
		SchemeDir: *schemeDir,
		OutDir:    *outDir,
		Package:   pkgName,
		Version:   version,
	}

	if err := generator.Run(cfg); err != nil {
		log.Fatalf("codegen failed: %v", err)
	}

	fmt.Printf("codegen: done — output in %s/%s/\n", *outDir, pkgName)
}

func sanitizePackageName(name string) string {
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, ".", "_")
	return strings.ToLower(name)
}
