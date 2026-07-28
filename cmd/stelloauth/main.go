package main

import (
	"log"

	"github.com/tamcore/stelloauth/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
