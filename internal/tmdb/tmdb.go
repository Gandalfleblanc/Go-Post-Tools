// Package tmdb : client pour le proxytmdb Elysium (auto-hébergé, format
// UKLM-compatible : ?t=search&q= / ?t=movie&q= / ?t=tv&q= / ?t=imdb&q= /
// ?t=providers&type=movie|tv&q=).
//
// Auth : header X-GPT-Token (baké au build). Base URL bakée aussi.
// Endpoint public : https://elysium-les5zamis.com/tmdb-api
package tmdb

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Bakés au build (peuvent être surchargés via ldflags si besoin).
var (
	DefaultProxyURL   = "https://elysium-les5zamis.com/tmdb-api"
	DefaultProxyToken = "627815568b8668cfddedc24e2f74dbce40e87480cc2637e7"
)

type Client struct {
	base       string
	token      string
	httpClient *http.Client
}

// NewClient : signature rétrocompat (apiKey ignoré — auth via token bakée).
func NewClient(_ string) *Client {
	return NewClientWithBase("")
}

// NewClientWithBase : accepte un override d'URL. Vide → DefaultProxyURL.
func NewClientWithBase(baseURL string) *Client {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = DefaultProxyURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	return &Client{
		base:       baseURL,
		token:      DefaultProxyToken,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// WithAPIKey : rétrocompat, no-op (auth via header token désormais).
func (c *Client) WithAPIKey(_ string) *Client { return c }

// Movie : structure compatible TMDB officiel + champs bonus du proxy
// (ImdbID, NoteImdb, VoteImdb).
type Movie struct {
	ID            int     `json:"id"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"original_title"`
	Name          string  `json:"name"`
	OriginalName  string  `json:"original_name"`
	Overview      string  `json:"overview"`
	PosterPath    string  `json:"poster_path"`
	ReleaseDate   string  `json:"release_date"`
	FirstAirDate  string  `json:"first_air_date"`
	VoteAverage   float64 `json:"vote_average"`
	MediaType     string  `json:"media_type"`
	ImdbID        string  `json:"imdb_id,omitempty"`
	NoteImdb      float64 `json:"note_imdb,omitempty"`
	VoteImdb      int     `json:"vote_imdb,omitempty"`
}

// UnmarshalJSON : tolérant au type des champs numériques. Le proxy UKLM
// retourne parfois note_imdb / vote_imdb / vote_average sous forme de string
// (ex: "" ou "7.2") au lieu de number. On accepte les deux formats.
func (m *Movie) UnmarshalJSON(data []byte) error {
	type movieAlias Movie
	aux := &struct {
		NoteImdb    json.RawMessage `json:"note_imdb"`
		VoteImdb    json.RawMessage `json:"vote_imdb"`
		VoteAverage json.RawMessage `json:"vote_average"`
		*movieAlias
	}{movieAlias: (*movieAlias)(m)}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	m.NoteImdb = parseFlexFloat(aux.NoteImdb)
	m.VoteImdb = int(parseFlexFloat(aux.VoteImdb))
	m.VoteAverage = parseFlexFloat(aux.VoteAverage)
	return nil
}

// parseFlexFloat : décode un json.RawMessage en float64, accepte number ou
// string (ou null / "" → 0). Tolérant aux erreurs (retourne 0 plutôt que err).
func parseFlexFloat(raw json.RawMessage) float64 {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return 0
		}
		if s == "" {
			return 0
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0
		}
		return v
	}
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0
	}
	return v
}

func (m *Movie) DisplayTitle() string {
	if m.Title != "" {
		return m.Title
	}
	if m.Name != "" {
		return m.Name
	}
	if m.OriginalTitle != "" {
		return m.OriginalTitle
	}
	return m.OriginalName
}

func (m *Movie) Year() string {
	d := m.ReleaseDate
	if d == "" {
		d = m.FirstAirDate
	}
	if len(d) >= 4 {
		return d[:4]
	}
	return ""
}

func (m *Movie) PosterURL() string {
	if m.PosterPath == "" {
		return ""
	}
	return "https://image.tmdb.org/t/p/w500" + m.PosterPath
}

// searchHit : shape interne renvoyée par /api?t=search (différente de TMDB).
type searchHit struct {
	Title         string  `json:"title"`
	OriginalTitle string  `json:"original_title"`
	Years         int     `json:"years"`
	PosterPath    string  `json:"poster_path"`
	Genres        string  `json:"genres"`
	Runtime       string  `json:"runtime"`
	ImdbID        string  `json:"imdb_id"`
	NoteImdb      float64 `json:"note_imdb"`
	VoteImdb      int     `json:"vote_imdb"`
	TmdbID        int     `json:"tmdb_id"`
	TmdbURL       string  `json:"tmdb_url"`             // ex: https://www.themoviedb.org/tv/254528 → detect movie vs tv
	NoteTmdb      float64 `json:"note_tmdb"`
	Overview      string  `json:"overview"`
	Season        string  `json:"season"`               // ex: "S01E10" si série
	MediaType     string  `json:"media_type,omitempty"` // parfois exposé directement
}

// UnmarshalJSON sur searchHit : même tolérance que Movie (proxy parfois
// retourne note_imdb/note_tmdb/vote_imdb en string).
func (h *searchHit) UnmarshalJSON(data []byte) error {
	type hitAlias searchHit
	aux := &struct {
		NoteImdb json.RawMessage `json:"note_imdb"`
		VoteImdb json.RawMessage `json:"vote_imdb"`
		NoteTmdb json.RawMessage `json:"note_tmdb"`
		*hitAlias
	}{hitAlias: (*hitAlias)(h)}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	h.NoteImdb = parseFlexFloat(aux.NoteImdb)
	h.VoteImdb = int(parseFlexFloat(aux.VoteImdb))
	h.NoteTmdb = parseFlexFloat(aux.NoteTmdb)
	return nil
}

func (h searchHit) toMovie() Movie {
	m := Movie{
		ID:          h.TmdbID,
		Title:       h.Title,
		Overview:    h.Overview,
		PosterPath:  h.PosterPath,
		VoteAverage: h.NoteTmdb,
		MediaType:   h.MediaType,
		ImdbID:      h.ImdbID,
		NoteImdb:    h.NoteImdb,
		VoteImdb:    h.VoteImdb,
	}
	if h.Years > 0 {
		m.ReleaseDate = fmt.Sprintf("%d-01-01", h.Years)
	}
	// Détection movie vs tv depuis tmdb_url (le champ media_type est rarement
	// présent dans les search hits). Ex: .../movie/12345 → movie, .../tv/6789 → tv.
	// CRITIQUE : les tmdb_id sont indépendants entre movie et tv, appeler le
	// mauvais endpoint retourne une fiche complètement différente.
	if m.MediaType == "" {
		if strings.Contains(h.TmdbURL, "/tv/") {
			m.MediaType = "tv"
		} else {
			m.MediaType = "movie"
		}
	}
	// Séries : FirstAirDate au lieu de ReleaseDate
	if m.MediaType == "tv" && m.ReleaseDate != "" {
		m.FirstAirDate = m.ReleaseDate
		m.ReleaseDate = ""
		if m.Name == "" {
			m.Name = m.Title
		}
	}
	return m
}

// doGet : GET sur le proxytmdb (format UKLM ?t=X&q=Y) avec header X-GPT-Token.
func (c *Client) doGet(params url.Values) ([]byte, error) {
	req, err := http.NewRequest("GET", c.base+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "GoPostTools/6.x")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-GPT-Token", c.token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("proxytmdb HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return body, nil
}

// Search : proxytmdb /api?t=search&q=<query>. Retourne les hits parsés
// depuis le format UKLM et convertis en Movie.
func (c *Client) Search(query string) ([]Movie, error) {
	params := url.Values{}
	params.Set("t", "search")
	params.Set("q", query)
	body, err := c.doGet(params)
	if err != nil {
		return nil, err
	}
	var resBody struct {
		Results []searchHit `json:"results"`
		Error   string      `json:"error"`
	}
	if err := json.Unmarshal(body, &resBody); err != nil {
		return nil, fmt.Errorf("proxytmdb parse: %w", err)
	}
	if resBody.Error != "" {
		return nil, fmt.Errorf("proxytmdb: %s", resBody.Error)
	}
	out := make([]Movie, 0, len(resBody.Results))
	for _, h := range resBody.Results {
		out = append(out, h.toMovie())
	}
	return out, nil
}

// GetByID : proxytmdb /api?t=movie|tv&q=<id>. Renvoie le JSON TMDB standard.
func (c *Client) GetByID(id int, mediaType string) (*Movie, error) {
	if mediaType != "movie" && mediaType != "tv" {
		mediaType = "movie"
	}
	params := url.Values{}
	params.Set("t", mediaType)
	params.Set("q", strconv.Itoa(id))
	body, err := c.doGet(params)
	if err != nil {
		return nil, err
	}
	var movie Movie
	if err := json.Unmarshal(body, &movie); err != nil {
		return nil, err
	}
	movie.MediaType = mediaType
	return &movie, nil
}

// GetByImdbID : proxytmdb /api?t=imdb&q=<imdb_id>. Renvoie une fiche TMDB
// directe (déjà résolue par le proxy via l'API /find/ officielle).
func (c *Client) GetByImdbID(imdbID string) (*Movie, error) {
	params := url.Values{}
	params.Set("t", "imdb")
	params.Set("q", imdbID)
	body, err := c.doGet(params)
	if err != nil {
		return nil, err
	}
	var movie Movie
	if err := json.Unmarshal(body, &movie); err != nil {
		return nil, err
	}
	if movie.FirstAirDate != "" {
		movie.MediaType = "tv"
	} else {
		movie.MediaType = "movie"
	}
	return &movie, nil
}

// Provider : un service de streaming (Netflix, Disney+, etc.).
type Provider struct {
	LogoPath     string `json:"logo_path"`
	ProviderID   int    `json:"provider_id"`
	ProviderName string `json:"provider_name"`
}

// CountryProviders : ce qui est dispo dans un pays donné.
type CountryProviders struct {
	Link      string     `json:"link"`
	Flatrate  []Provider `json:"flatrate,omitempty"` // streaming inclus dans abonnement
	Buy       []Provider `json:"buy,omitempty"`
	Rent      []Provider `json:"rent,omitempty"`
	Free      []Provider `json:"free,omitempty"`
}

// GetProviders : proxytmdb /api?t=providers&type=movie|tv&q=<id>.
func (c *Client) GetProviders(tmdbID int, mediaType string) (map[string]CountryProviders, error) {
	if mediaType != "movie" && mediaType != "tv" {
		mediaType = "movie"
	}
	params := url.Values{}
	params.Set("t", "providers")
	params.Set("type", mediaType)
	params.Set("q", strconv.Itoa(tmdbID))
	body, err := c.doGet(params)
	if err != nil {
		return nil, err
	}
	var resBody struct {
		Results map[string]CountryProviders `json:"results"`
	}
	if err := json.Unmarshal(body, &resBody); err != nil {
		return nil, err
	}
	return resBody.Results, nil
}

// TestConnection : ping du proxytmdb (recherche triviale) — valide token + URL.
func (c *Client) TestConnection() error {
	params := url.Values{}
	params.Set("t", "search")
	params.Set("q", "Inception 2010")
	_, err := c.doGet(params)
	return err
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
