// Package igdb : client API IGDB (Internet Game Database, via Twitch OAuth2).
//
// Auth : OAuth2 client_credentials sur https://id.twitch.tv/oauth2/token →
// access_token (valable ~60j, mis en cache + auto-refresh). Chaque requête
// IGDB porte les headers Client-ID + Authorization: Bearer.
//
// Recherche : POST https://api.igdb.com/v4/games avec le langage Apicalypse
// (corps texte brut, pas du JSON) :
//
//	search "zelda"; fields name,summary,cover.image_id,first_release_date,
//	platforms.name,genres.name,total_rating; limit 20;
package igdb

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	tokenURL  = "https://id.twitch.tv/oauth2/token"
	apiBase   = "https://api.igdb.com/v4"
	userAgent = "GoPostTools/6.x"
)

type Client struct {
	clientID string
	secret   string
	http     *http.Client

	mu        sync.Mutex
	token     string
	tokenExp  time.Time
}

func NewClient(clientID, secret string) *Client {
	return &Client{
		clientID: clientID,
		secret:   secret,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

// ensureToken récupère (ou rafraîchit) le bearer token OAuth2 Twitch.
func (c *Client) ensureToken() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.tokenExp.Add(-5*time.Minute)) {
		return c.token, nil
	}
	if c.clientID == "" || c.secret == "" {
		return "", fmt.Errorf("credentials IGDB manquants (Client ID / Secret)")
	}
	form := url.Values{}
	form.Set("client_id", c.clientID)
	form.Set("client_secret", c.secret)
	form.Set("grant_type", "client_credentials")
	req, err := http.NewRequest("POST", tokenURL+"?"+form.Encode(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("oauth twitch: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("oauth twitch HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", fmt.Errorf("parse token: %w", err)
	}
	if tok.AccessToken == "" {
		return "", fmt.Errorf("token vide (vérifie Client ID/Secret)")
	}
	c.token = tok.AccessToken
	c.tokenExp = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	return c.token, nil
}

func (c *Client) query(endpoint, apicalypse string) ([]byte, error) {
	token, err := c.ensureToken()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", apiBase+endpoint, strings.NewReader(apicalypse))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Client-ID", c.clientID)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("igdb HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return body, nil
}

// Game : métadonnées d'un jeu. Les champs *Raw servent à parser la réponse
// IGDB ; les champs propres (CoverImageID, Year, Platforms, CoverURL) sont
// remplis par enrich() et exposés au frontend via les bindings Wails.
type Game struct {
	ID             int     `json:"id"`
	Name           string  `json:"name"`
	Summary        string  `json:"summary"`
	FirstReleaseAt int64   `json:"first_release_date"` // unix timestamp
	TotalRating    float64 `json:"total_rating"`       // 0-100

	// Champs dérivés (remplis par enrich, exposés au front)
	CoverImageID string   `json:"CoverImageID"`
	CoverURL     string   `json:"CoverURL"`
	Year         string   `json:"Year"`
	Platforms    []string `json:"Platforms"`
	Genres       []string `json:"Genres"`

	// Champs bruts imbriqués IGDB (consommés par enrich)
	Cover *struct {
		ImageID string `json:"image_id"`
	} `json:"cover,omitempty"`
	PlatformsRaw []struct {
		Name string `json:"name"`
	} `json:"platforms,omitempty"`
	GenresRaw []struct {
		Name string `json:"name"`
	} `json:"genres,omitempty"`
}

func enrich(games []Game) []Game {
	for i := range games {
		g := &games[i]
		if g.Cover != nil {
			g.CoverImageID = g.Cover.ImageID
			if g.CoverImageID != "" {
				g.CoverURL = "https://images.igdb.com/igdb/image/upload/t_cover_big/" + g.CoverImageID + ".jpg"
			}
		}
		if g.FirstReleaseAt > 0 {
			g.Year = time.Unix(g.FirstReleaseAt, 0).UTC().Format("2006")
		}
		for _, p := range g.PlatformsRaw {
			if p.Name != "" {
				g.Platforms = append(g.Platforms, p.Name)
			}
		}
		for _, gn := range g.GenresRaw {
			if gn.Name != "" {
				g.Genres = append(g.Genres, gn.Name)
			}
		}
	}
	return games
}

const gameFields = `fields name,summary,cover.image_id,first_release_date,platforms.name,genres.name,total_rating;`

// Search : recherche de jeux par nom.
func (c *Client) Search(queryStr string, limit int) ([]Game, error) {
	if strings.TrimSpace(queryStr) == "" {
		return nil, fmt.Errorf("requête vide")
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	// Échappe les guillemets dans la query
	q := strings.ReplaceAll(queryStr, `"`, `\"`)
	body := fmt.Sprintf(`search "%s"; %s limit %d;`, q, gameFields, limit)
	data, err := c.query("/games", body)
	if err != nil {
		return nil, err
	}
	var games []Game
	if err := json.Unmarshal(data, &games); err != nil {
		return nil, fmt.Errorf("parse games: %w", err)
	}
	return enrich(games), nil
}

// GetByID : récupère un jeu précis par son IGDB id.
func (c *Client) GetByID(igdbID int) (*Game, error) {
	body := fmt.Sprintf(`where id = %d; %s limit 1;`, igdbID, gameFields)
	data, err := c.query("/games", body)
	if err != nil {
		return nil, err
	}
	var games []Game
	if err := json.Unmarshal(data, &games); err != nil {
		return nil, fmt.Errorf("parse game: %w", err)
	}
	if len(games) == 0 {
		return nil, nil
	}
	g := enrich(games)[0]
	return &g, nil
}

// Ping : valide les credentials (récupère un token + 1 requête triviale).
func (c *Client) Ping() error {
	if _, err := c.ensureToken(); err != nil {
		return err
	}
	_, err := c.query("/games", `fields id; limit 1;`)
	return err
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
