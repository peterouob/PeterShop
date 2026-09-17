package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"github.com/peterouob/seckill_service/pkg/config"
	"github.com/peterouob/seckill_service/pkg/database"
)

func main() {
	var (
		source = flag.String("source", "file://deploy/migrations", "migration source url")
		dir    = flag.String("direction", "up", "up | down | version")
		steps  = flag.Int("steps", 0, "number of migrations to apply, 0 means all")
	)
	flag.Parse()

	if err := run(*source, *dir, *steps); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(source, dir string, steps int) error {
	_ = godotenv.Load()

	cfg, err := config.Load("migrate", config.SectionMySQL)
	if err != nil {
		return err
	}

	if dir == "version" {
		version, dirty, err := database.Version(cfg, source)
		if err != nil {
			return err
		}
		fmt.Printf("version=%d dirty=%t\n", version, dirty)
		return nil
	}

	direction := database.Direction(dir)
	if direction != database.Up && direction != database.Down {
		return database.ErrUnknownDirection
	}
	if err := database.Migrate(cfg, source, direction, steps); err != nil {
		return err
	}

	fmt.Printf("migrations applied: direction=%s steps=%d\n", direction, steps)
	return nil
}
