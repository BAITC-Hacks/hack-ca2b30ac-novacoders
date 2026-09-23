package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"
	"sort"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/demo"
)

func main() {
	output := flag.String("out", "data/demo", "Output directory for synthetic XLSX files")
	flag.Parse()
	files, err := demo.Workbooks()
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(*output, 0700); err != nil {
		log.Fatal(err)
	}
	fields := make([]string, 0, len(files))
	for field := range files {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	for _, field := range fields {
		if err := os.WriteFile(filepath.Join(*output, demo.Filenames[field]), files[field], 0600); err != nil {
			log.Fatal(err)
		}
	}
	log.Printf("Created %d synthetic XLSX files in %s", len(files), *output)
}
