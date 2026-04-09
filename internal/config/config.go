package config

import (
	"encoding/json"
	"os"
)

const rootPath = "/mnt/us"

type ProxyConfig struct {
	Enabled       bool   `json:"enabled"`
	Type          string `json:"type"` // "socks5", "http", "mtproto"
	Address       string `json:"address"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	MTProtoSecret string `json:"mtproto_secret"`
}

type TelegramUpdatesState struct {
	Pts  int `json:"pts"`
	Date int `json:"date"`
	Qts  int `json:"qts"`
}

type Config struct {
	BotToken          string               `json:"bot_token"`
	ChatID            int64                `json:"chat_id"`
	AllowedExtensions []string             `json:"allowed_extensions"`
	RootPath          string               `json:"root_path"`
	DownloadPath      string               `json:"download_path"`
	LastUpdateID      int                  `json:"last_update_id"`
	Proxy             ProxyConfig          `json:"proxy"`
	UpdatesState      TelegramUpdatesState `json:"updates_state"`
}

func DefaultConfig() *Config {
	return &Config{
		BotToken:          "",
		ChatID:            0,
		AllowedExtensions: []string{".epub", ".mobi", ".pdf", ".zip", ".fb2"},
		RootPath:          rootPath,
		DownloadPath:      "/mnt/us/books",
		LastUpdateID:      0,
		Proxy: ProxyConfig{
			Enabled:       false,
			Type:          "socks5",
			Address:       "",
			Username:      "",
			Password:      "",
			MTProtoSecret: "",
		},
		UpdatesState: TelegramUpdatesState{
			Pts:  0,
			Date: 0,
			Qts:  0,
		},
	}
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) Save(path string) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, b, 0644); err != nil {
		return err
	}

	return os.Rename(tmpPath, path)
}
