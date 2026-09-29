// Command registry-site renders models/registry.yml and its model cards into
// a static site (GitHub Pages).
//
//	go run ./cmd/registry-site -registry models/registry.yml -out site
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"ai-server/internal/site"
)

func main() {
	reg := flag.String("registry", "models/registry.yml", "registry file (readme paths are relative to it)")
	out := flag.String("out", "site", "output directory")
	title := flag.String("title", "self model registry", "site title")
	repo := flag.String("repo", os.Getenv("SITE_REPO_URL"), "source repository URL for edit/source links")
	flag.Parse()

	b, err := os.ReadFile(*reg)
	if err == nil {
		err = site.Build(b, os.DirFS(filepath.Dir(*reg)), *out, site.Options{Title: *title, RepoURL: *repo})
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "registry-site:", err)
		os.Exit(1)
	}
	fmt.Println("wrote", *out)
}
