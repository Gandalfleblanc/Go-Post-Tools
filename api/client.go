package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Client struct {
	token      string
	baseURL    string
	httpClient *http.Client
	ctxMu      sync.Mutex
	ctx        context.Context
}

func NewClient(token, baseURL string) *Client {
	return &Client{
		token:      token,
		baseURL:    normalizeBaseURL(baseURL),
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
}

// normalizeBaseURL garantit que le baseURL se termine par /api/v1 (sans slash final).
func normalizeBaseURL(u string) string {
	u = strings.TrimRight(u, "/")
	if u == "" {
		return ""
	}
	if !strings.HasSuffix(u, "/api/v1") {
		u += "/api/v1"
	}
	return u
}

// BaseURL retourne le baseURL API courant (ex: https://hydracker.com/api/v1).
func (c *Client) BaseURL() string { return c.baseURL }

// SiteURL retourne le domaine sans /api/v1 (ex: https://hydracker.com) pour liens UI.
func (c *Client) SiteURL() string {
	return strings.TrimSuffix(c.baseURL, "/api/v1")
}

func (c *Client) SetToken(token string) {
	c.token = token
}

// SetBaseURL met à jour le baseURL à chaud (ex: changement dans Settings).
func (c *Client) SetBaseURL(u string) {
	c.baseURL = normalizeBaseURL(u)
}

// SetContext définit le contexte utilisé pour les prochaines requêtes (annulation).
func (c *Client) SetContext(ctx context.Context) {
	c.ctxMu.Lock()
	c.ctx = ctx
	c.ctxMu.Unlock()
}

func (c *Client) getContext() context.Context {
	c.ctxMu.Lock()
	ctx := c.ctx
	c.ctxMu.Unlock()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (c *Client) do(method, path string, body any, params url.Values) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(b)
	}

	if c.baseURL == "" {
		return nil, fmt.Errorf("URL Hydracker non configurée (Réglages)")
	}
	reqURL := c.baseURL + path
	if len(params) > 0 {
		reqURL += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(c.getContext(), method, reqURL, bodyReader)
	if err != nil {
		return nil, err
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		return data, nil
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("unauthorized: vérifiez votre token")
	case http.StatusForbidden:
		return nil, fmt.Errorf("accès refusé (403)")
	case http.StatusNotFound:
		return nil, fmt.Errorf("ressource introuvable (404)")
	case http.StatusPaymentRequired:
		return nil, fmt.Errorf("solde insuffisant (402)")
	case http.StatusUnprocessableEntity:
		return nil, fmt.Errorf("données invalides (422): %s", string(data))
	default:
		return nil, fmt.Errorf("erreur HTTP %d: %s", resp.StatusCode, string(data))
	}
}

func (c *Client) get(path string, params url.Values) ([]byte, error) {
	return c.do("GET", path, nil, params)
}

func (c *Client) post(path string, body any) ([]byte, error) {
	return c.do("POST", path, body, nil)
}

func (c *Client) put(path string, body any) ([]byte, error) {
	return c.do("PUT", path, body, nil)
}

func (c *Client) delete(path string) ([]byte, error) {
	return c.do("DELETE", path, nil, nil)
}

func intParam(v int) string {
	return strconv.Itoa(v)
}
