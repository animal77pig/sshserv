package main

import (
	"fmt"
	"github.com/melbahja/goph"
	"log"
)

func main() {

	// Start new ssh connection with private key.
	auth, err := goph.Key("/home/laog/.ssh/id_rsa", "")
	if err != nil {
		log.Fatal(err)
	}

	client, err := goph.NewUnknown("app", "121.43.230.103", auth)  //New
	if err != nil {
		log.Fatal(err)
	}

	// Defer closing the network connection.
	defer client.Close()

	// Execute your command.
	out, err := client.Run("ls /tmp/")

	if err != nil {
		log.Fatal(err)
	}

	// Get your output as []byte.
	fmt.Println(string(out))
}

