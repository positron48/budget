// tinvestcheck verifies Keychain/API access without displaying the credential.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/user"
	"time"

	"github.com/positron48/budget/internal/adapter/tinvest"
)

func run() error {
	query := flag.String("query", "SBER", "public instrument query")
	flag.Parse()
	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("cannot determine current macOS account")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	c, err := tinvest.NewFromKeychain(ctx, u.Username)
	if err != nil {
		return err
	}
	items, err := c.FindInstrument(ctx, *query)
	if err != nil {
		return err
	}
	fmt.Printf("Keychain access: OK; T-Invest FindInstrument: OK; matches=%d\n", len(items))
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
