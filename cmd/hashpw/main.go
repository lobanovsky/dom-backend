package main

import (
	"fmt"
	"log"
	"os"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	log.SetFlags(0)
	if len(os.Args) != 2 {
		log.Fatal("usage: hashpw <password>")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(os.Args[1]), bcrypt.DefaultCost)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := fmt.Println(string(h)); err != nil {
		log.Fatal(err)
	}
}
