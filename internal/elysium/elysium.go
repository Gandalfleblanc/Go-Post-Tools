// Package elysium : client API v1 pour le tracker Elysium (cross-post depuis
// GO Post Tools après un post Hydracker réussi). L'endpoint /api/v1/upload
// accepte un NZB (multipart) et/ou une URL DDL 1Fichier, plus les métadonnées.
//
// Auth : header Authorization: Bearer els_<48 hex>. Chaque user génère sa
// propre clé sur elysium-les5zamis.com/account-settings.
package elysium

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const DefaultBaseURL = "https://elysium-les5zamis.com"

type Client struct {
	base  string
	token string
	http  *http.Client
}

func NewClient(token string) *Client {
	return &Client{
		base:  DefaultBaseURL,
		token: strings.TrimSpace(token),
		http:  &http.Client{Timeout: 5 * time.Minute}, // upload NZB peut prendre du temps
	}
}

type User struct {
	ID              int     `json:"id"`
	Name            string  `json:"name"`
	Role            string  `json:"role"`
	Ratio           *float64 `json:"ratio"`
	RatioUploaded   int64    `json:"ratio_uploaded"`
	RatioDownloaded int64    `json:"ratio_downloaded"`
}

// Me : ping /api/v1/me pour valider le token.
func (c *Client) Me() (*User, error) {
	if c.token == "" {
		return nil, fmt.Errorf("token Elysium manquant")
	}
	req, _ := http.NewRequest("GET", c.base+"/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GoPostTools/7.x")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("elysium /me HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var u User
	if err := json.Unmarshal(body, &u); err != nil {
		return nil, fmt.Errorf("parse /me: %w", err)
	}
	return &u, nil
}

// TitleLookup : cherche un title Elysium par tmdb_id.
// Retourne l'id Elysium (title_id à utiliser dans /upload) ou 0 si inexistant.
type Title struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	Year    int    `json:"year"`
	Type    string `json:"type"`
	TmdbID  int    `json:"tmdb_id"`
	IgdbID  int    `json:"igdb_id"`
}

func (c *Client) GetTitleByTmdbID(tmdbID int) (*Title, error) {
	return c.getTitleByExternal("tmdb_id", strconv.Itoa(tmdbID))
}

func (c *Client) GetTitleByIgdbID(igdbID int) (*Title, error) {
	return c.getTitleByExternal("igdb_id", strconv.Itoa(igdbID))
}

func (c *Client) getTitleByExternal(param, value string) (*Title, error) {
	if c.token == "" {
		return nil, fmt.Errorf("token Elysium manquant")
	}
	params := url.Values{}
	params.Set(param, value)
	req, _ := http.NewRequest("GET", c.base+"/api/v1/titles?"+params.Encode(), nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GoPostTools/7.x")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("elysium /titles HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var resp2 struct {
		Data []Title `json:"data"`
	}
	if err := json.Unmarshal(body, &resp2); err != nil {
		return nil, fmt.Errorf("parse /titles: %w", err)
	}
	if len(resp2.Data) == 0 {
		return nil, nil
	}
	return &resp2.Data[0], nil
}

// UploadPayload : entrée de Upload — décrit ce qui va être posté sur Elysium.
type UploadPayload struct {
	TitleID     int      // Elysium title id (via GetTitleByTmdbID/IgdbID)
	CategoryID  int      // 1=Films, 2=Séries, 3=Jeux, 4=Musique, 5=Ebooks
	SizeBytes   int64    // Taille du .mkv/.zip source
	FileName    string   // Nom du release (sans .mkv)
	Quality     string   // Nom exact TrackerMeta (ex: "WEB 1080p (x265)")
	Languages   []string // Ex: ["TrueFrench", "English"]
	Subtitles   []string // Ex: ["French", "English"]
	Description string   // NFO / description libre
	DDLURL      string   // URL 1fichier.com (optionnel)
	NZBFilePath string   // Chemin local du .nzb à uploader (optionnel)
}

type UploadResult struct {
	Nzb *struct {
		ID       int    `json:"id"`
		Status   string `json:"status"`
		FileName string `json:"file_name"`
	} `json:"nzb"`
	Ddl *struct {
		ID       int    `json:"id"`
		Status   string `json:"status"`
		FileName string `json:"file_name"`
	} `json:"ddl"`
	Message string `json:"message"`
}

// Upload : POST /api/v1/upload avec nzb_file (multipart) et/ou ddl_url.
// Au moins un des deux (NZB ou DDL) doit être fourni.
func (c *Client) Upload(p UploadPayload) (*UploadResult, error) {
	if c.token == "" {
		return nil, fmt.Errorf("token Elysium manquant")
	}
	if p.NZBFilePath == "" && p.DDLURL == "" {
		return nil, fmt.Errorf("upload Elysium : au moins un des NZB ou DDL requis")
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("title_id", strconv.Itoa(p.TitleID))
	_ = w.WriteField("category_id", strconv.Itoa(p.CategoryID))
	_ = w.WriteField("size_bytes", strconv.FormatInt(p.SizeBytes, 10))
	if p.FileName != "" {
		_ = w.WriteField("file_name", p.FileName)
	}
	if p.Quality != "" {
		_ = w.WriteField("quality", p.Quality)
	}
	for _, l := range p.Languages {
		_ = w.WriteField("language[]", l)
	}
	for _, s := range p.Subtitles {
		_ = w.WriteField("subtitles[]", s)
	}
	if p.Description != "" {
		_ = w.WriteField("description", p.Description)
	}
	if p.DDLURL != "" {
		_ = w.WriteField("ddl_url", p.DDLURL)
	}
	if p.NZBFilePath != "" {
		f, err := os.Open(p.NZBFilePath)
		if err != nil {
			return nil, fmt.Errorf("ouverture NZB : %w", err)
		}
		defer f.Close()
		part, err := w.CreateFormFile("nzb_file", filepath.Base(p.NZBFilePath))
		if err != nil {
			return nil, err
		}
		if _, err := io.Copy(part, f); err != nil {
			return nil, fmt.Errorf("copie NZB : %w", err)
		}
	}
	w.Close()

	req, _ := http.NewRequest("POST", c.base+"/api/v1/upload", &buf)
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GoPostTools/7.x")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return nil, fmt.Errorf("elysium /upload HTTP %d: %s", resp.StatusCode, truncate(string(body), 400))
	}
	var out UploadResult
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("parse /upload: %w", err)
	}
	return &out, nil
}

// CategoryFromMediaType : mappe le media_type TMDB/IGDB vers un category_id Elysium.
func CategoryFromMediaType(mediaType string) int {
	switch strings.ToLower(mediaType) {
	case "tv", "series":
		return 2 // Séries
	case "game":
		return 3 // Jeux
	case "music":
		return 4 // Musique
	case "ebook":
		return 5 // Ebooks
	default:
		return 1 // Films (défaut)
	}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
