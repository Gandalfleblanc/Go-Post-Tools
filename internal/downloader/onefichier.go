// Package downloader fournit des helpers pour télécharger depuis les hébergeurs
// de type 1Fichier, Send.now, etc. via leurs API premium. Utilisé pour le
// workflow auto-reseed DDL → FTP (stream direct, pas de disque local).
package downloader

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OneFichierGetToken appelle l'API 1fichier pour obtenir une URL de
// téléchargement direct depuis une URL partage 1fichier. Requiert une clé API
// premium configurée dans Réglages.
//
// Endpoint : POST https://api.1fichier.com/v1/download/get_token.cgi
// Body     : {"url": "https://1fichier.com/?xxx"}
// Auth     : Bearer <apikey>
// Retour   : {"status":"OK","url":"https://a-1.1fichier.com/..."}
func OneFichierGetToken(ctx context.Context, apiKey, shareURL string) (string, error) {
	if apiKey == "" {
		return "", fmt.Errorf("clé API 1fichier manquante (Réglages)")
	}
	if !strings.Contains(shareURL, "1fichier.com") {
		return "", fmt.Errorf("URL non-1fichier : %s", shareURL)
	}
	body, _ := json.Marshal(map[string]string{"url": shareURL})
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.1fichier.com/v1/download/get_token.cgi", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	c := &http.Client{Timeout: 30 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var r struct {
		Status  string `json:"status"`
		URL     string `json:"url"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return "", fmt.Errorf("réponse 1fichier invalide: %s", string(data))
	}
	if r.Status != "OK" || r.URL == "" {
		return "", fmt.Errorf("1fichier: %s", r.Message)
	}
	return r.URL, nil
}

// OneFichierGetInfo récupère les métadonnées d'un lien 1fichier (nom du fichier,
// taille, etc.) sans le télécharger. Utilisé dans l'UI Fiches pour afficher
// le vrai nom du fichier derrière l'URL partagée.
//
// Endpoint : POST https://api.1fichier.com/v1/file/info.cgi
// Body     : {"url": "https://1fichier.com/?xxx"}
type OneFichierInfo struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	Date     string `json:"date"`
}

func OneFichierGetInfo(ctx context.Context, apiKey, shareURL string) (*OneFichierInfo, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("clé API 1fichier manquante")
	}
	if !strings.Contains(shareURL, "1fichier.com") {
		return nil, fmt.Errorf("URL non-1fichier : %s", shareURL)
	}
	body, _ := json.Marshal(map[string]string{"url": shareURL})
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.1fichier.com/v1/file/info.cgi", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	c := &http.Client{Timeout: 15 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var info OneFichierInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("réponse 1fichier invalide: %s", string(data))
	}
	if info.Filename == "" {
		return nil, fmt.Errorf("1fichier: filename vide (lien mort ou protégé)")
	}
	return &info, nil
}

// Progress rapporte l'avancement d'un téléchargement.
type Progress struct {
	Bytes   int64   `json:"bytes"`
	Total   int64   `json:"total"`
	Percent float64 `json:"percent"`
	SpeedMB float64 `json:"speed_mb"`
}

// StreamDownload retourne un io.ReadCloser sur le contenu de l'URL + la taille
// totale. Le caller est responsable de fermer le reader.
// Pas de progress-wrapping ici — laissé au caller (ex: via un progressReader
// autour du reader retourné).
func StreamDownload(ctx context.Context, directURL string) (io.ReadCloser, int64, string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", directURL, nil)
	if err != nil {
		return nil, 0, "", err
	}
	c := &http.Client{
		// Pas de timeout global — les gros fichiers peuvent prendre des heures.
		// Le ctx gère l'annulation.
		Timeout: 0,
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, 0, "", err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, 0, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	// Extrait le nom depuis Content-Disposition si possible
	filename := ""
	cd := resp.Header.Get("Content-Disposition")
	if cd != "" {
		if idx := strings.Index(cd, `filename="`); idx != -1 {
			rest := cd[idx+10:]
			if end := strings.Index(rest, `"`); end != -1 {
				filename = rest[:end]
			}
		}
	}
	return resp.Body, resp.ContentLength, filename, nil
}

// ProgressReader wrappe un io.Reader et émet un callback de progression toutes
// les 250ms (throttle identique à l'uploader).
type ProgressReader struct {
	R          io.Reader
	Total      int64
	OnProgress func(Progress)
	bytes      int64
	start      time.Time
	lastEmit   time.Time
}

func (pr *ProgressReader) Read(p []byte) (int, error) {
	n, err := pr.R.Read(p)
	if n > 0 {
		pr.bytes += int64(n)
		now := time.Now()
		if pr.start.IsZero() {
			pr.start = now
			pr.lastEmit = now
		}
		if now.Sub(pr.lastEmit) >= 250*time.Millisecond && pr.OnProgress != nil {
			pr.lastEmit = now
			elapsed := now.Sub(pr.start).Seconds()
			var speed float64
			if elapsed > 0 {
				speed = float64(pr.bytes) / elapsed / 1e6
			}
			var pct float64
			if pr.Total > 0 {
				pct = float64(pr.bytes) / float64(pr.Total) * 100
				if pct > 99 {
					pct = 99
				}
			}
			pr.OnProgress(Progress{
				Bytes:   pr.bytes,
				Total:   pr.Total,
				Percent: pct,
				SpeedMB: speed,
			})
		}
	}
	return n, err
}
