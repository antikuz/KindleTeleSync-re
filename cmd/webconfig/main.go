package main

import (
	"embed"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/antikuz/KindleTeleSync-re/internal/config"
	"github.com/skip2/go-qrcode"
)

//go:embed templates/*
var tmplFS embed.FS

var (
	rootPath   = "/mnt/us/extensions/KindleTeleSync"
	configPath = filepath.Join(rootPath, "config.json")
)

func addIptablesRule() error {
	cmd := exec.Command("iptables", "-I", "INPUT", "-p", "tcp", "--dport", "8880", "-j", "ACCEPT")
	return cmd.Run()
}

func removeIptablesRule() error {
	cmd := exec.Command("iptables", "-D", "INPUT", "-p", "tcp", "--dport", "8880", "-j", "ACCEPT")
	return cmd.Run()
}

func main() {
	if p := os.Getenv("KINDLE_ROOT"); p != "" {
		rootPath = p
		configPath = filepath.Join(rootPath, "config.json")
	}

	if len(os.Args) == 3 && os.Args[1] == "qr" {
		err := qrcode.WriteFile(os.Args[2], qrcode.Medium, 350, "qr.png")
		if err != nil {
			log.Fatalf("Failed to generate QR: %v", err)
		}
		log.Println("QR code saved to qr.png")
		return
	}

	if _, err := exec.LookPath("iptables"); err == nil {
		if err := addIptablesRule(); err != nil {
			log.Fatalf("Warning: failed to add iptables rule: %v", err)
		}
		defer func() {
			if err := removeIptablesRule(); err != nil {
				log.Fatalf("Warning: failed to remove iptables rule: %v", err)
			}
		}()
    }

	cfg, err := config.Load(configPath)
	if err != nil {
		cfg = config.DefaultConfig()
	}

	templates := template.Must(template.ParseFS(tmplFS, "templates/*.html"))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		cfg.AllowedExtensions = []string{strings.Join(cfg.AllowedExtensions, ", ")}
		templates.ExecuteTemplate(w, "index.html", cfg)
	})

	http.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		botToken := r.FormValue("bot_token")
		chatID, _ := strconv.ParseInt(r.FormValue("chat_id"), 10, 64)
		
		// reset saved states if bot_token or chat_id changes
		if cfg.ChatID != chatID || cfg.BotToken != botToken {
			cfg.UpdatesState.Pts = 0
			cfg.UpdatesState.Date = 0
			cfg.UpdatesState.Qts = 0
		}

		cfg.BotToken = botToken
		cfg.ChatID = chatID

		exts := strings.Split(r.FormValue("allowed_extensions"), ",")
		for i := range exts {
			exts[i] = strings.TrimSpace(exts[i])
		}

		cfg.AllowedExtensions = exts
		cfg.DownloadPath = r.FormValue("download_path")
		cfg.Proxy.Enabled = r.FormValue("proxy_enabled") == "on"
		cfg.Proxy.Type = r.FormValue("proxy_type")
		cfg.Proxy.Address = r.FormValue("proxy_address")
		cfg.Proxy.Username = r.FormValue("proxy_username")
		cfg.Proxy.Password = r.FormValue("proxy_password")
		cfg.Proxy.MTProtoSecret = r.FormValue("proxy_mtproto_secret")

		if err := cfg.Save(configPath); err != nil {
			http.Error(w, "Failed to save config: "+err.Error(), 500)
			return
		}

		templates.ExecuteTemplate(w, "save.html", nil)

		go func() {
			log.Println("Configuration saved, shutting down...")
			os.Exit(0)
		}()
	})

	http.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
		go func() {
			log.Println("Stopping server...")
			os.Exit(0)
		}()
	})

	log.Println("Starting webconfig on :8880")
	log.Fatal(http.ListenAndServe(":8880", nil))
}
