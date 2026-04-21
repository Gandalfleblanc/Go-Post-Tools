package seedbox

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Progress struct {
	Percent float64 `json:"percent"`
	SpeedMB float64 `json:"speed_mb"`
}

// Upload pousse un .torrent vers ruTorrent via addtorrent.php (HTTP Basic Auth).
// baseURL : URL de base de ruTorrent (ex: https://my-seedbox.example/rutorrent/).
// label : label optionnel à appliquer au torrent (ruTorrent).
func Upload(ctx context.Context, baseURL, user, password, label, torrentPath string, onProgress func(Progress)) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if baseURL == "" {
		return "", fmt.Errorf("URL seedbox manquante")
	}
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	addTorrentURL := baseURL + "php/addtorrent.php"

	f, err := os.Open(torrentPath)
	if err != nil {
		return "", fmt.Errorf("ouverture %s: %w", torrentPath, err)
	}
	defer f.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("torrent_file", filepath.Base(torrentPath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, f); err != nil {
		return "", err
	}
	if label != "" {
		_ = w.WriteField("label", label)
	}
	_ = w.WriteField("tadd_label", label)
	w.Close()

	req, err := http.NewRequestWithContext(ctx, "POST", addTorrentURL, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if user != "" {
		req.SetBasicAuth(user, password)
	}

	if onProgress != nil {
		onProgress(Progress{Percent: 50})
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload ruTorrent: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == 401 {
		return "", fmt.Errorf("authentification refusée (401) — vérifiez user/password")
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	// ruTorrent renvoie du HTML avec noty(..., "success"|"error"). On détecte le statut.
	bodyStr := string(body)
	lc := strings.ToLower(bodyStr)
	if strings.Contains(lc, "addtorrentfailed") || strings.Contains(lc, ",\"error\"") {
		return "", fmt.Errorf("ruTorrent a refusé : %s", strings.TrimSpace(bodyStr))
	}

	if onProgress != nil {
		onProgress(Progress{Percent: 100})
	}
	return addTorrentURL, nil
}

// Ping vérifie l'accès à l'interface ruTorrent (HTTP 200 sur la racine).
func Ping(baseURL, user, password string) error {
	if baseURL == "" {
		return fmt.Errorf("URL manquante")
	}
	req, _ := http.NewRequest("GET", baseURL, nil)
	if user != "" {
		req.SetBasicAuth(user, password)
	}
	c := &http.Client{Timeout: 10 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 {
		return fmt.Errorf("auth refusée (401)")
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
