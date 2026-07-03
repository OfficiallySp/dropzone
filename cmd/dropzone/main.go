// Command dropzone is a system-tray applet that watches for screenshots and screen
// recordings, compresses them, and uploads them to Cloudflare R2.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"dropzone/internal/autostart"
	"dropzone/internal/clipboardwatch"
	"dropzone/internal/compress"
	"dropzone/internal/config"
	"dropzone/internal/pipeline"
	"dropzone/internal/r2"
	"dropzone/internal/tray"
	"dropzone/internal/watcher"
)

func main() {
	setup := flag.Bool("setup", false, "run interactive setup and exit")
	verify := flag.Bool("verify", false, "verify R2 credentials and exit")
	flag.Parse()

	log.SetFlags(log.LstdFlags)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	if *setup || !cfg.Configured() {
		if err := runSetup(cfg); err != nil {
			log.Fatalf("setup: %v", err)
		}
		if *setup {
			return
		}
		// Reload so the freshly stored secret is picked up.
		if cfg, err = config.Load(); err != nil {
			log.Fatalf("reload config: %v", err)
		}
	}

	if *verify {
		if err := verifyR2(cfg); err != nil {
			log.Fatalf("verify: %v", err)
		}
		fmt.Println("R2 credentials OK — bucket reachable.")
		return
	}

	run(cfg)
}

func run(cfg *config.Config) {
	ctx, cancel := context.WithCancel(context.Background())

	secret, err := cfg.Secret()
	if err != nil {
		log.Fatalf("read secret: %v", err)
	}
	client, err := r2.New(ctx, cfg.R2.AccountID, cfg.R2.AccessKeyID, secret, cfg.R2.Bucket, cfg.R2.PublicBaseURL)
	if err != nil {
		log.Fatalf("r2 client: %v", err)
	}

	pipe := pipeline.New(cfg, client)
	pipe.OnStatus = tray.SetStatus

	folders := cfg.ResolvedWatchFolders()
	clipOK := false
	if cfg.WatchClipboard {
		if err := clipboardwatch.Init(); err != nil {
			log.Printf("clipboard unavailable, watching files only: %v", err)
		} else {
			clipOK = true
		}
	}

	tray.Run(tray.Callbacks{
		Tooltip:          "dropzone — screenshot/video uploader",
		Icon:             tray.Icon(),
		AutostartEnabled: autostart.IsEnabled,
		OnReady: func() {
			pipe.Start(ctx, 3)
			startWatching(ctx, pipe, folders, clipOK)

			msg := fmt.Sprintf("watching %d folder(s)", len(folders))
			if clipOK {
				msg += " + clipboard"
			}
			if !compress.HasFFmpeg(cfg.Video.FFmpegPath) {
				msg += " (no ffmpeg: videos won't compress)"
			}
			tray.SetStatus(msg)
			log.Printf("dropzone started: %s; folders=%v", msg, folders)
		},
		OnPauseToggle: func(paused bool) {
			pipe.SetPaused(paused)
			if paused {
				tray.SetStatus("paused")
			} else {
				tray.SetStatus("watching")
			}
		},
		OnOpenConfig:      func() { _ = config.OpenPath(config.Dir()) },
		OnToggleAutostart: func(enable bool) (bool, error) { return autostart.Set(enable) },
		OnQuit:            func() { cancel() },
	})

	cancel()
}

func startWatching(ctx context.Context, pipe *pipeline.Pipeline, folders []string, clipOK bool) {
	w, err := watcher.New(folders)
	if err != nil {
		log.Printf("watcher: %v", err)
	} else {
		go w.Run(ctx)
		go func() {
			for ev := range w.Events() {
				pipe.SubmitFile(ev.Path, ev.Kind)
			}
		}()
	}

	if clipOK {
		ch := clipboardwatch.Watch(ctx)
		go func() {
			for data := range ch {
				pipe.SubmitImageBytes(data, "clipboard.png")
			}
		}()
	}
}

// --- setup / verify helpers ---

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
