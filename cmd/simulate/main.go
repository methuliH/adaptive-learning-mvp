// Command simulate runs fake students through the adaptive engine, prints how
// well it tracked them, and writes their answers as a pyBKT-ready CSV.
package main

import (
	"flag"
	"log"
	"os"

	"github.com/methuliH/adaptive-learning-mvp/sim"
)

func main() {
	cfg := sim.DefaultConfig
	flag.IntVar(&cfg.Students, "students", cfg.Students, "number of simulated students")
	flag.IntVar(&cfg.Steps, "steps", cfg.Steps, "answers per student")
	flag.Int64Var(&cfg.Seed, "seed", cfg.Seed, "random seed")
	out := flag.String("out", "sim_answers.csv", "pyBKT CSV output path (empty to skip)")
	flag.Parse()

	res, err := sim.Run(cfg)
	if err != nil {
		log.Fatal(err)
	}
	res.Report(os.Stdout, cfg)

	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		if err := res.Service.ExportCSV(f); err != nil {
			log.Fatal(err)
		}
		log.Printf("wrote %s", *out)
	}
}
