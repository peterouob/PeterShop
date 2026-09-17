package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

func main() {
	serviceName := flag.String("name", "", "Name of the service")
	flag.Parse()

	if *serviceName == "" {
		log.Println("please provide a service name using -name flag")
		os.Exit(1)
	}

	basePath := filepath.Join("service", *serviceName+"-service")
	dirs := []string{
		"cmd",
		"internal/model",
		"internal/service",
		"internal/repository",
		filepath.Join("internal", *serviceName+"grpc"),
	}

	for _, dir := range dirs {
		fullPath := filepath.Join(basePath, dir)
		if err := os.MkdirAll(fullPath, 0755); err != nil {
			log.Printf("Error creating directory %s:%v\n", dir, err)
			os.Exit(1)
		}
	}

	layout := fmt.Sprintf(`service/%s-service/
├── cmd/
│   └── main.go          # entrypoint: fx wiring for this service only
└── internal/            # private to this service, never imported by others
    ├── model/           # domain types and DTOs
    ├── service/         # business logic (use cases)
    ├── repository/      # persistence: MySQL, Redis, Lua scripts
    └── %sgrpc/          # gRPC handler: proto <-> domain, error -> status code
`, *serviceName, *serviceName)

	readmeContent := fmt.Sprintf(`# %s service

This service handles all %s-related operations in the system.

## Layout

`+"```"+`
%s`+"```"+`

## Rules

1. **One process, one service.** `+"`cmd/main.go`"+` composes only this service's
   fx module plus shared modules from the repo root `+"`pkg/`"+`.
2. **No cross-service imports.** Services talk to each other over gRPC using the
   generated clients in `+"`api/`"+`, never by importing another service's package.
   `+"`internal/`"+` is enforced by the Go toolchain.
3. **Dependencies point inward.** `+"`%sgrpc`"+` depends on `+"`service`"+`,
   `+"`service`"+` depends on the `+"`repository`"+` interface it declares.
   Nothing in `+"`service`"+` imports gRPC or gorm types.
4. **One package per layer, not one directory per noun.** Add a package only when
   it is a replaceable boundary you can test on its own.
5. **Shared code goes to the repo root `+"`pkg/`"+`**, and only once a second
   service actually needs it.
`, *serviceName, *serviceName, layout, *serviceName)

	readmePath := filepath.Join(basePath, "README.md")
	if err := os.WriteFile(readmePath, []byte(readmeContent), 0644); err != nil {
		fmt.Printf("Error creating README.md: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully created %s service structure in %s\n", *serviceName, basePath)
	fmt.Println("\nDirectory structure created:")
	fmt.Println(layout)
}
