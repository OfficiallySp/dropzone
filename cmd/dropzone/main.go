// Command dropzone is a desktop app (GUI + system tray) that watches for
// screenshots and screen recordings, compresses them, and uploads them to
// Cloudflare R2.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"dropzone/internal/config"
	"dropzone/internal/r2"
	"dropzone/internal/service"
	"dropzone/internal/ui"
)

func main() {
	setup := flag.Bool("setup", false, "run interactive CLI setup and exit")
	verify := flag.Bool("verify", false, "verify R2 credentials and exit")
	flag.Parse()

	log.SetFlags(log.LstdFlags)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	switch {
	case *setup:
		if err := runSetup(cfg); err != nil {
			log.Fatalf("setup: %v", err)
		}
	case *verify:
		if err := verifyR2(cfg); err != nil {
			log.Fatalf("verify: %v", err)
		}
		fmt.Println("R2 credentials OK — bucket reachable.")
	default:
		// GUI mode (default): the window/tray also serves as setup.
		ui.Run(cfg, service.New())
	}
}

// --- optional headless CLI setup / verify ---

func runSetup(cfg *config.Config) error {
	fmt.Println("dropzone setup — enter your Cloudflare R2 details.")
	fmt.Println("(press Enter to keep the shown [current] value)")
	sc := bufio.NewScanner(os.Stdin)

	ask := func(prompt, cur string, secret bool) string {
		shown := cur
		if secret && cur != "" {
			shown = "********"
		}
		if shown != "" {
			fmt.Printf("%s [%s]: ", prompt, shown)
		} else {
			fmt.Printf("%s: ", prompt)
		}
		if !sc.Scan() {
			return cur
		}
		v := strings.TrimSpace(sc.Text())
		if v == "" {
			return cur
		}
		return v
	}

	cfg.R2.AccountID = ask("Cloudflare Account ID", cfg.R2.AccountID, false)
	cfg.R2.AccessKeyID = ask("R2 Access Key ID", cfg.R2.AccessKeyID, false)
	newSecret := ask("R2 Secret Access Key", "", true)
	cfg.R2.Bucket = ask("Bucket name", cfg.R2.Bucket, false)
	cfg.R2.PublicBaseURL = ask("Public base URL (custom domain, e.g. https://cdn.example.com)", cfg.R2.PublicBaseURL, false)

	cfg.ApplyDefaults()

	if newSecret != "" {
		if err := cfg.SetSecret(newSecret); err != nil {
			log.Printf("keychain unavailable, storing secret in config file: %v", err)
		}
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("Saved config to %s\n", cfg.PathUsed())

	if cfg.Configured() {
		if err := verifyR2(cfg); err != nil {
			fmt.Printf("Warning: could not verify bucket access: %v\n", err)
		} else {
			fmt.Println("Verified: bucket is reachable.")
		}
	}
	return nil
}

func verifyR2(cfg *config.Config) error {
	secret, err := cfg.Secret()
	if err != nil {
		return err
	}
	client, err := r2.New(context.Background(), cfg.R2.AccountID, cfg.R2.AccessKeyID, secret, cfg.R2.Bucket, cfg.R2.PublicBaseURL)
	if err != nil {
		return err
	}
	return client.Verify(context.Background())
}
