package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var RepoPath = "antikuz/KindleTeleSync-re"

type GithubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []GithubAsset `json:"assets"`
}

type GithubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadUrl string `json:"browser_download_url"`
}

func init() {
	log.SetFlags(0)
	log.SetPrefix(fmt.Sprintf("[%s] ", time.Now().Format("2006-01-02 15:04:05")))
}

func safeJoin(baseDir, name string) (string, error) {
	target := filepath.Join(baseDir, name)
	rel, err := filepath.Rel(baseDir, target)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("unsafe path in archive: %q", name)
	}
	return target, nil
}

func main() {
	rootPath := "/mnt/us"
	if p := os.Getenv("KINDLE_ROOT"); p != "" {
		rootPath = p
	}

	workingDirPath := filepath.Join(rootPath, "extensions/KindleTeleSync")

	versionFile := filepath.Join(workingDirPath, "version.txt")
	currentVerBytes, _ := os.ReadFile(versionFile)
	currentVer := strings.TrimSpace(string(currentVerBytes))

	resp, err := http.Get(fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", RepoPath))
	if err != nil {
		log.Fatalf("Failed to fetch updates: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("GitHub API returned status %d", resp.StatusCode)
	}

	var release GithubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		log.Fatalf("Failed to parse response: %v", err)
	}

	latestVer := strings.TrimPrefix(release.TagName, "v")
	if currentVer != "" && currentVer == latestVer {
		log.Printf("Already up to date (%s). Exiting.", currentVer)
		os.Exit(0)
	}

	log.Printf("New version found: %s. Downloading...", latestVer)

	targetArch := "armv7"
	if goarm := os.Getenv("GOARM"); goarm != "" {
		targetArch = "armv" + goarm
	}

	var assetUrl string
	for _, asset := range release.Assets {
		if strings.Contains(asset.Name, targetArch) && strings.HasSuffix(asset.Name, ".tar.gz") {
			assetUrl = asset.BrowserDownloadUrl
			break
		}
	}

	if assetUrl == "" {
		log.Fatalf("No matching asset found for architecture %s", targetArch)
	}

	dlResp, err := http.Get(assetUrl)
	if err != nil {
		log.Fatalf("Failed to download asset: %v", err)
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode != http.StatusOK {
		log.Fatalf("Asset download returned status %d", dlResp.StatusCode)
	}

	gzr, err := gzip.NewReader(dlResp.Body)
	if err != nil {
		log.Fatalf("Failed to open gzip stream: %v", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	tmpUpdateDir := "/tmp/update_kindle_sync"

	if err := os.RemoveAll(tmpUpdateDir); err != nil {
		log.Fatalf("Failed to remove temp dir: %v", err)
	}
	if err := os.MkdirAll(tmpUpdateDir, 0755); err != nil {
		log.Fatalf("Failed to create temp dir: %v", err)
	}

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("Tar read error: %v", err)
		}

		target, err := safeJoin(tmpUpdateDir, header.Name)
		if err != nil {
			log.Fatalf("Skipping unsafe tar entry: %v", err)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				log.Fatalf("Failed to create directory %s: %v", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				log.Fatalf("Failed to create parent dir for %s: %v", target, err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				log.Fatalf("Failed to create file %s: %v", target, err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				log.Fatalf("Failed to extract %s: %v", target, err)
			}
			f.Close()
		}
	}

	err = filepath.Walk(tmpUpdateDir, func(srcPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if srcPath == tmpUpdateDir {
			return nil
		}

		rel, err := filepath.Rel(tmpUpdateDir, srcPath)
		if err != nil {
			return err
		}

		parts := strings.SplitN(rel, string(filepath.Separator), 2)
		if len(parts) == 2 {
			rel = parts[1]
		}
		if rel == "" {
			return nil
		}

		if rel == "config.json" {
			if _, err := os.Stat(filepath.Join(workingDirPath, "config.json")); err == nil {
				log.Printf("Skipping config.json (user config exists)")
				return nil
			}
		}

		dst := filepath.Join(rootPath, rel)

		if info.IsDir() {
			return os.MkdirAll(dst, info.Mode())
		}

		srcFile, err := os.Open(srcPath)
		if err != nil {
			return fmt.Errorf("open source %s: %w", srcPath, err)
		}
		defer srcFile.Close()

		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return fmt.Errorf("mkdir for %s: %w", dst, err)
		}

		dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return fmt.Errorf("open dest %s: %w", dst, err)
		}
		defer dstFile.Close()

		if _, err := io.Copy(dstFile, srcFile); err != nil {
			return fmt.Errorf("copy %s: %w", rel, err)
		}

		log.Printf("Updated %s", rel)
		return nil
	})
	if err != nil {
		log.Fatalf("Failed to apply update: %v", err)
	}

	if err := os.WriteFile(versionFile, []byte(latestVer), 0644); err != nil {
		log.Fatalf("Failed to write version file: %v", err)
	}
	if err := os.RemoveAll(tmpUpdateDir); err != nil {
		log.Printf("Warning: failed to remove temp dir: %v", err)
	}

	log.Println("Update completed successfully.")
}