package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Arylite/netprobe/internal/central/store"
)

// loadCipher reads the key that NETPROBE_SECRET_KEY_FILE names, if any. A key is
// never a flag or a variable: both are visible to every user of the machine.
func loadCipher() (*store.Cipher, error) {
	file := os.Getenv("NETPROBE_SECRET_KEY_FILE")
	if file == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("read the secret key file: %w", err)
	}
	key, err := store.ParseKey(string(raw))
	if err != nil {
		return nil, err
	}
	return store.NewCipher(key)
}

// secretKeyCommand prints a new key, to keep in the file NETPROBE_SECRET_KEY_FILE names.
func secretKeyCommand(_ []string, out io.Writer) error {
	key, err := store.GenerateKey()
	if err != nil {
		return err
	}
	fmt.Fprintln(out, key)
	return nil
}

func secretsCommand(args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "encrypt" {
		return errUsage
	}
	fs := flag.NewFlagSet("secrets encrypt", flag.ContinueOnError)
	databaseURL := databaseFlag(fs)
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if os.Getenv("NETPROBE_SECRET_KEY_FILE") == "" {
		return errors.New("set NETPROBE_SECRET_KEY_FILE to a file that holds a key: 'netprobe-central secret-key' makes one")
	}
	st, err := openStore(context.Background(), *databaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	n, err := st.EncryptChannels(context.Background())
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%d channels encrypted\n", n)
	return nil
}
