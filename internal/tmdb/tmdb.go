package tmdb

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const base = "https://api.themoviedb.org/3"

type Client struct {
	apiKey     string
	httpClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

type Movie struct {
	ID          int     `json:"id"`
	Title       string  `json:"title"`
	Name        string  `json:"name"`
	Overview    string  `json:"overview"`
	PosterPath  string  `json:"poster_path"`
	ReleaseDate string  `json:"release_date"`
	FirstAirDate string `json:"first_air_date"`
	VoteAverage float64 `json:"vote_average"`
	MediaType   string  `json:"media_type"`
}

func (m *Movie) DisplayTitle() string {
	if m.Title != "" {
		return m.Title
	}
	return m.Name
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

type SearchResult struct {
	Results []Movie `json:"results"`
}

// isBearerToken détecte si la clé est un JWT Bearer token (commence par "eyJ")
func (c *Client) isBearerToken() bool {
	return strings.HasPrefix(c.apiKey, "eyJ")
}

// newRequest crée une requête avec le bon type d'auth selon la clé
func (c *Client) newRequest(endpoint string, params url.Values) (*http.Request, error) {
	if c.isBearerToken() {
		req, err := http.NewRequest("GET", base+endpoint+"?"+params.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		return req, nil
	}
	params.Set("api_key", c.apiKey)
	return http.NewRequest("GET", base+endpoint+"?"+params.Encode(), nil)
}

func (c *Client) Search(query string) ([]Movie, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("language", "fr-FR")

	req, err := c.newRequest("/search/multi", params)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("TMDB erreur %d: %s", resp.StatusCode, string(body))
	}

	var result SearchResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result.Results, nil
}

func (c *Client) GetByID(id int, mediaType string) (*Movie, error) {
	if mediaType == "" {
		mediaType = "movie"
	}
	params := url.Values{}
	params.Set("language", "fr-FR")

	req, err := c.newRequest(fmt.Sprintf("/%s/%d", mediaType, id), params)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("TMDB erreur %d: %s", resp.StatusCode, string(body))
	}

	var movie Movie
	movie.MediaType = mediaType
	if err := json.Unmarshal(body, &movie); err != nil {
		return nil, err
	}
	return &movie, nil
}

func (c *Client) TestConnection() error {
	req, err := c.newRequest("/configuration", url.Values{})
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("clé API invalide (HTTP %d)", resp.StatusCode)
	}
	return nil
}
