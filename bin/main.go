package main

import (
	"errors"
	"flag"
	"fmt"
	"go-sshd"
	"golang.org/x/crypto/ssh"
	"log"
	"os"
	"os/signal"
)

var (
	user        = flag.String("user", "foo", "user name")
	keyPassword = flag.String("keyPassword", "1234", "key pass")
	password    = flag.String("password", "bar", "user password")
	address     = flag.String("address", "localhost:2022", "listen address")
	hostKeyPath = flag.String("host-key", default_(), "the path of the host private key")
	shell       = flag.String("shell", "bash", "path of shell")
)

func default_() string {
	home, _ := os.UserHomeDir()

	return fmt.Sprintf("%s%s%s", home, "/.ssh/", "id_rsa")
}

func main() {
	flag.Parse()

	config := &ssh.ServerConfig{
		//Define a function to run when a client attempts a password login
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			// Should use constant-time compare (or better, salt+hash) in a production setting.
			if c.User() == *user && string(pass) == *password {
				return nil, nil
			}
			return nil, fmt.Errorf("password rejected for %q", c.User())
		},
		// You may also explicitly allow anonymous client authentication, though anon bash
		// sessions may not be a wise idea
		//NoClientAuth: true,
	}

	privateBytes, err := os.ReadFile(*hostKeyPath)
	if err != nil {
		log.Fatalf("Failed to load private key (%s); %s", *hostKeyPath, err)
	}

	private, err := ssh.ParsePrivateKey(privateBytes)
	var passphraseMissingError *ssh.PassphraseMissingError
	if errors.As(err, &passphraseMissingError) {
		private, err = ssh.ParsePrivateKeyWithPassphrase(privateBytes, []byte(*keyPassword))
		if err != nil {
			log.Fatalf("Failed to parse private key (%s); %s", *hostKeyPath, err)
		}
	}

	config.AddHostKey(private)

	logger := log.New(os.Stdout, "", 0)

	srv := sshd.NewServer(
		*shell,
		config,
		logger,
	)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		fmt.Println("Got signal. Now stop the server")
		_ = srv.Close()
	}()

	fmt.Printf("Listening on %s\n", *address)
	log.Fatal(srv.ListenAndServe(*address))
}
