package main

import (
	"log"

	"tasq-backend/internal/server"
)

func main() {
	if err := server.Run(); err != nil {
		log.Fatal(err)
	}
}
