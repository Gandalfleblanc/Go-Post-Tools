// Package tmdb : client pour l'API TMDB officielle (api.themoviedb.org/3).
// L'ancien proxy tmdb.uklm.xyz est mort et retiré.
//
// Auth : query param ?api_key=<v3_api_key> sur chaque requête.
// Récupérer la clé sur themoviedb.org → Settings → API.
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

const defaultBase = "https://api.themoviedb.org/3"

type Client struct {
	base       string
	apiKey     string
	httpClient *http.Client
}

// NewClient : client TMDB officiel avec clé API v3.
func NewClient(apiKey string) *Client {
	return &Client{
		base:       defaultBase,
		apiKey:     strings.TrimSpace(apiKey),
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// NewClientWithBase : override la base URL (utile si l'user veut un proxy
// custom compatible TMDB v3). Signature gardée pour rétrocompat des callers ;
// l'apiKey vient de cfg.TMDBApiKey côté app.go.
func NewClientWithBase(baseURL string) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultBase
	}
	baseURL = strings.TrimRight(baseURL, "/")
	return &Client{
		base:       baseURL,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// WithAPIKey : setter fluent pour attacher la clé après création
// (utilisé par app.go quand la clé n'est pas dispo au moment de la construction).
func (c *Client) WithAPIKey(apiKey string) *Client {
	c.apiKey = strings.TrimSpace(apiKey)
	return c
}

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
	NoteTmdb      float64 `json:"note_tmdb"`
	Overview      string  `json:"overview"`
	MediaType     string  `json:"media_type,omitempty"` // "movie" ou "tv" si proxy l'expose
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
	if m.MediaType == "" {
		m.MediaType = "movie" // défaut
	}
	return m
}

// doGet : GET sur l'API TMDB officielle avec ?api_key=... en query.
func (c *Client) doGet(path string, extraParams url.Values) ([]byte, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("clé API TMDB manquante (Réglages → TMDB)")
	}
	params := url.Values{}
	if extraParams != nil {
		for k, v := range extraParams {
			params[k] = v
		}
	}
	params.Set("api_key", c.apiKey)
	req, err := http.NewRequest("GET", c.base+path+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "GoPostTools/6.x")
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("tmdb HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return body, nil
}

// Search : /3/search/multi — retourne films + séries mixés.
// L'API officielle n'exige pas d'année dans la query (contrairement à UKLM).
func (c *Client) Search(query string) ([]Movie, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("include_adult", "false")
	body, err := c.doGet("/search/multi", params)
	if err != nil {
		return nil, err
	}
	var resBody struct {
		Results []Movie `json:"results"`
	}
	if err := json.Unmarshal(body, &resBody); err != nil {
		return nil, fmt.Errorf("tmdb parse: %w", err)
	}
	// Filtre les résultats de type "person" (media_type = person)
	out := make([]Movie, 0, len(resBody.Results))
	for _, m := range resBody.Results {
		if m.MediaType == "person" {
			continue
		}
		if m.MediaType == "" {
			m.MediaType = "movie"
		}
		out = append(out, m)
	}
	return out, nil
}

// GetByID : /3/movie/{id} ou /3/tv/{id}.
func (c *Client) GetByID(id int, mediaType string) (*Movie, error) {
	if mediaType != "movie" && mediaType != "tv" {
		mediaType = "movie"
	}
	body, err := c.doGet(fmt.Sprintf("/%s/%d", mediaType, id), nil)
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

// GetByImdbID : /3/find/{imdb_id}?external_source=imdb_id.
// Réponse : {movie_results:[...], tv_results:[...]}.
func (c *Client) GetByImdbID(imdbID string) (*Movie, error) {
	params := url.Values{}
	params.Set("external_source", "imdb_id")
	body, err := c.doGet("/find/"+imdbID, params)
	if err != nil {
		return nil, err
	}
	var wrap struct {
		MovieResults []Movie `json:"movie_results"`
		TVResults    []Movie `json:"tv_results"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		return nil, err
	}
	if len(wrap.MovieResults) > 0 {
		m := wrap.MovieResults[0]
		m.MediaType = "movie"
		return &m, nil
	}
	if len(wrap.TVResults) > 0 {
		m := wrap.TVResults[0]
		m.MediaType = "tv"
		return &m, nil
	}
	return nil, fmt.Errorf("aucun résultat TMDB pour IMDb %s", imdbID)
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

// GetProviders : /3/{movie|tv}/{id}/watch/providers.
// Retourne map[country_code]CountryProviders. Pour la France, key = "FR".
func (c *Client) GetProviders(tmdbID int, mediaType string) (map[string]CountryProviders, error) {
	if mediaType != "movie" && mediaType != "tv" {
		mediaType = "movie"
	}
	body, err := c.doGet(fmt.Sprintf("/%s/%d/watch/providers", mediaType, tmdbID), nil)
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

// TestConnection : ping /3/authentication?api_key=... — valide la clé.
func (c *Client) TestConnection() error {
	if c.apiKey == "" {
		return fmt.Errorf("clé API TMDB manquante")
	}
	req, err := http.NewRequest("GET", c.base+"/authentication?api_key="+url.QueryEscape(c.apiKey), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "GoPostTools/6.x")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("proxytmdb HTTP %d", resp.StatusCode)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
