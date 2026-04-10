package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/antikuz/KindleTeleSync-re/internal/config"
	"github.com/beevik/ntp"
	"github.com/celestix/gotgproto"
	"github.com/celestix/gotgproto/sessionMaker"
	"github.com/gotd/td/telegram/dcs"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/message/html"
	"github.com/gotd/td/tg"
	"golang.org/x/net/proxy"
)

const (
	// Default open source Telegram App ID/Hash
	appID   = 2040
	appHash = "b18441a1ff607e10a989891a5462e627"
)

func init() {
	log.SetFlags(0)
	log.SetPrefix(fmt.Sprintf("[%s] ", time.Now().Format("2006-01-02 15:04:05")))
}

// mtproto highly depends on correct date
func syncClock() {
    servers := []string{
		"ru.pool.ntp.org",
        "pool.ntp.org",
        "time.cloudflare.com", 
        "time.google.com",
    }
    
    for _, server := range servers {
        t, err := ntp.Time(server)
        if err != nil {
            log.Printf("NTP %s failed: %v", server, err)
            continue
        }
        
        tv := syscall.Timeval{
            Sec:  t.Unix(),
            Usec: int32(t.Nanosecond() / 1000),
        }
        if err := syscall.Settimeofday(&tv); err != nil {
            log.Printf("Settimeofday failed: %v", err)
            return
        }
        
        log.Printf("Clock synced via %s", server)
        return
    }
    
    log.Printf("All NTP servers failed, continuing with system time")
}

// handles all MTProto, SOCKS5, and HTTP proxy logic
func setupResolver(cfg *config.Config) dcs.Resolver {
	if !cfg.Proxy.Enabled {
		return dcs.DefaultResolver()
	}

	switch cfg.Proxy.Type {
	case "mtproto":
		secret, err := hex.DecodeString(cfg.Proxy.MTProtoSecret)
		if err != nil {
			secret, err = base64.RawURLEncoding.DecodeString(cfg.Proxy.MTProtoSecret)
		}
		if err != nil || len(secret) == 0 {
			log.Fatalf("Invalid MTProxy secret format")
		}
		resolver, err := dcs.MTProxy(cfg.Proxy.Address, secret, dcs.MTProxyOptions{})
		if err != nil {
			log.Fatalf("MTProxy config error: %v", err)
		}
		return resolver

	case "socks5":
		var auth *proxy.Auth
		if cfg.Proxy.Username != "" || cfg.Proxy.Password != "" {
			auth = &proxy.Auth{User: cfg.Proxy.Username, Password: cfg.Proxy.Password}
		}
		dialer, err := proxy.SOCKS5("tcp", cfg.Proxy.Address, auth, proxy.Direct)
		if err != nil {
			log.Fatalf("SOCKS5 config error: %v", err)
		}
		return dcs.Plain(dcs.PlainOptions{
			Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.Dial(network, addr)
			},
		})

	case "http":
		return dcs.Plain(dcs.PlainOptions{
			Dial: httpProxyDialer(cfg.Proxy.Address, cfg.Proxy.Username, cfg.Proxy.Password),
		})
	}
	return dcs.DefaultResolver()
}

func httpProxyDialer(proxyAddr, user, pass string) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := net.Dial(network, proxyAddr)
		if err != nil {
			return nil, err
		}
		req := &http.Request{
			Method: "CONNECT",
			URL:    &url.URL{Opaque: addr},
			Host:   addr,
			Header: make(http.Header),
		}
		if user != "" || pass != "" {
			auth := user + ":" + pass
			req.Header.Add("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(auth)))
		}
		if err := req.Write(conn); err != nil {
			return nil, err
		}

		br := make([]byte, 1024)
		_, err = conn.Read(br)
		if err != nil || !strings.Contains(string(br), "200") {
			return nil, fmt.Errorf("HTTP Proxy error: %v", string(br))
		}
		return conn, nil
	}
}

func getUniqueFilename(baseDir, filename string) string {
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	outPath := filepath.Join(baseDir, filename)

	for counter := 1; ; counter++ {
		if _, err := os.Stat(outPath); os.IsNotExist(err) {
			break
		}
		outPath = filepath.Join(baseDir, fmt.Sprintf("%s_%d%s", base, counter, ext))
	}
	return outPath
}

func getFilename(doc *tg.Document) string {
	for _, attr := range doc.Attributes {
		if nameAttr, ok := attr.(*tg.DocumentAttributeFilename); ok {
			return nameAttr.FileName
		}
	}
	return ""
}

func getPeerID(peer interface{}) int64 {
	switch p := peer.(type) {
	case *tg.PeerUser:
		return p.UserID
	case *tg.PeerChat:
		return p.ChatID
	case *tg.PeerChannel:
		return p.ChannelID
	default:
		return 0
	}
}

func isAllowedExt(filename string, allowed []string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	for _, aExt := range allowed {
		if strings.ToLower(strings.TrimSpace(aExt)) == ext {
			return true
		}
	}
	return false
}

func processMessages(ctx context.Context, api *tg.Client, sender *message.Sender, dl *downloader.Downloader, messages []tg.MessageClass, peer tg.InputPeerClass, cfg *config.Config) {
	successDownloads := []string{}
	failedDownloads := []string{}
	for _, m := range messages {
		msg, ok := m.(*tg.Message)
		if !ok {
			continue
		}

		if getPeerID(msg.PeerID) != peer.(*tg.InputPeerUser).UserID {
			continue
		}

		media, ok := msg.Media.(*tg.MessageMediaDocument)
		if !ok {
			continue
		}
		doc, ok := media.Document.(*tg.Document)
		if !ok {
			continue
		}

		filename := getFilename(doc)
		if filename == "" || !isAllowedExt(filename, cfg.AllowedExtensions) {
			continue
		}

		outPath := getUniqueFilename(cfg.DownloadPath, filename)
		loc := doc.AsInputDocumentFileLocation()

		if _, err := dl.Download(api, loc).ToPath(ctx, outPath); err != nil {
			log.Printf("Failed to download %s: %v", filename, err)
			failedDownloads = append(failedDownloads, filename)
			continue
		}

		log.Printf("Downloaded: %s", outPath)
		successDownloads = append(successDownloads, filename)
	}

	text := ""
	if len(successDownloads) > 0 {
		text = fmt.Sprintf("Сохранено %d файлов:\n\n<code>%s</code> \n\n", len(successDownloads), strings.Join(successDownloads, "\n"))
	}

	if len(failedDownloads) > 0 {
		if len(successDownloads) > 0 {
			text += "\n\n"
		}
		text += fmt.Sprintf("Ошибки скачивания %d файлов:\n\n<code>%s</code>\n\n", len(failedDownloads), strings.Join(failedDownloads, "\n"))
	}

	if text == "" {
		return
	}

	if _, err := sender.To(peer).StyledText(ctx, html.String(nil, text)); err != nil {
		log.Printf("Failed to send notification: %v", err)
	}
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	syncClock()
	
	rootPath := config.DefaultConfig().RootPath
	if p := os.Getenv("KINDLE_ROOT"); p != "" {
		rootPath = p
	}

    workingDirPath := filepath.Join(rootPath, "extensions/KindleTeleSync")
	configPath := filepath.Join(workingDirPath, "config.json")
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if _, err := os.Stat(cfg.DownloadPath); os.IsNotExist(err) {
		if err = os.MkdirAll(cfg.DownloadPath, 0755); err != nil {
			log.Fatalf("Failed to create %s directory: %v", cfg.DownloadPath, err)
		}
	}

	type clientResult struct {
		client *gotgproto.Client
		err    error
	}
	ch := make(chan clientResult, 1)

	opts := &gotgproto.ClientOpts{
		InMemory:         true,
		Session:          sessionMaker.SimpleSession(),
		Resolver:         setupResolver(cfg),
		DialTimeout:      120 * time.Second,
		DisableCopyright: true,
	}
	go func() {
		c, err := gotgproto.NewClient(
			appID,
			appHash,
			gotgproto.ClientTypeBot(cfg.BotToken),
			opts,
		)
		ch <- clientResult{c, err}
	}()

	var client *gotgproto.Client
	select {
	case <-ctx.Done():
		log.Fatalf("Timeout waiting for bot login: %v", ctx.Err())
	case res := <-ch:
		if res.err != nil {
			log.Fatalf("Bot login failed: %v", res.err)
		}
		client = res.client
	}
	defer client.Stop()

	log.Println("Bot logged in successfully.")
	api := client.API()
	sender := message.NewSender(api)
	peer := &tg.InputPeerUser{UserID: cfg.ChatID, AccessHash: 0}

	if cfg.UpdatesState.Pts == 0 {
		state, err := api.UpdatesGetState(ctx)
		if err != nil {
			log.Fatalf("Failed to get state: %v", err)
		}

		log.Printf("First run: initializing PTS state to %d", state.Pts)
		cfg.UpdatesState = config.TelegramUpdatesState{
			Pts:  state.Pts,
			Date: state.Date,
			Qts:  state.Qts,
		}

		if err := cfg.Save(configPath); err != nil {
			log.Fatalf("Failed to save config: %v", err)
		}

		log.Println("Initialized successfully. Waiting for new messages on next run.")

		text := fmt.Sprintf("Чат %d успешно инициализирован", cfg.ChatID)
		if _, err := sender.To(peer).StyledText(ctx, html.String(nil, text)); err != nil {
			log.Printf("Failed to send notification: %v", err)
		}

		return
	}

	diff, err := api.UpdatesGetDifference(ctx, &tg.UpdatesGetDifferenceRequest{
		Pts:  cfg.UpdatesState.Pts,
		Date: cfg.UpdatesState.Date,
		Qts:  cfg.UpdatesState.Qts,
	})
	if err != nil {
		log.Fatalf("Failed to get updates difference: %v", err)
	}

	var msgs []tg.MessageClass
	var newPts int

	switch d := diff.(type) {
	case *tg.UpdatesDifferenceEmpty:
		log.Println("No new messages.")
	case *tg.UpdatesDifference:
		msgs = d.NewMessages
		newPts = d.State.Pts
	case *tg.UpdatesDifferenceSlice:
		msgs = d.NewMessages
		newPts = d.IntermediateState.Pts
	case *tg.UpdatesDifferenceTooLong:
		log.Println("Updates gap is too long. Skipping missed messages to catch up.")
		newPts = d.Pts
	default:
		log.Printf("Unexpected updates difference type: %T", diff)
	}

	if len(msgs) > 0 {
		dl := downloader.NewDownloader()
		processMessages(ctx, api, sender, dl, msgs, peer, cfg)
	}

	if newPts > cfg.UpdatesState.Pts {
		cfg.UpdatesState.Pts = newPts
		cfg.UpdatesState.Date = int(time.Now().Unix())
		if err := cfg.Save(configPath); err != nil {
			log.Fatalf("Failed to save config: %v\n", err)
		}
	}

	log.Println("Sync completed successfully.")
}
