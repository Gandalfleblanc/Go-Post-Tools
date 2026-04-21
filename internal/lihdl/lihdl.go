package lihdl

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

type File struct {
	Name string
	URL  string
}

var (
	cacheMu      sync.Mutex
	cachedFiles  []File
	cachedAt     time.Time
	cacheTTL     = 24 * time.Hour
	hrefMkvRegex = regexp.MustCompile(`(?i)<a\s+href="([^"]+\.mkv)"`)
)

// FetchIndex retourne la liste des .mkv disponibles sur le dossier LiHDL.
// baseURL est l'URL du dossier (ex: https://exemple.tld/chemin/LiHDL/).
// La réponse est cachée pendant 24 h (refresh forcé si `force`).
func FetchIndex(force bool, baseURL, user, password string) ([]File, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("URL LiHDL non configurée (Réglages)")
	}
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	indexURL := baseURL + "?C=M;O=D"

	cacheMu.Lock()
	if !force && cachedFiles != nil && time.Since(cachedAt) < cacheTTL {
		defer cacheMu.Unlock()
		return cachedFiles, nil
	}
	cacheMu.Unlock()

	req, _ := http.NewRequest("GET", indexURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	if user != "" {
		req.SetBasicAuth(user, password)
	}
	c := &http.Client{Timeout: 30 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch LiHDL index: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d sur %s", resp.StatusCode, indexURL)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	matches := hrefMkvRegex.FindAllStringSubmatch(string(body), -1)
	files := make([]File, 0, len(matches))
	seen := make(map[string]bool)
	for _, m := range matches {
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		files = append(files, File{
			Name: name,
			URL:  baseURL + name,
		})
	}

	cacheMu.Lock()
	cachedFiles = files
	cachedAt = time.Now()
	cacheMu.Unlock()
	return files, nil
}

// Match cherche dans l'index un .mkv dont le nom correspond à la release donnée.
// Le matching est fait par nom exact (après normalisation), puis par préfixe.
func Match(releaseName string, files []File) *File {
	target := normalize(releaseName)
	// Tente .mkv exact
	for i, f := range files {
		if normalize(strings.TrimSuffix(f.Name, ".mkv")) == target {
			return &files[i]
		}
	}
	// Fallback : préfixe
	for i, f := range files {
		name := normalize(strings.TrimSuffix(f.Name, ".mkv"))
		if strings.HasPrefix(name, target) || strings.HasPrefix(target, name) {
			return &files[i]
		}
	}
	return nil
}

func normalize(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", ".")
	s = strings.ReplaceAll(s, "_", ".")
	return s
}
