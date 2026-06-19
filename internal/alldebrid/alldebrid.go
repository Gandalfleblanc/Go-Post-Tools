// Package alldebrid : client API pour AllDebrid (débrideur multi-hoster).
//
// Endpoints : https://docs.alldebrid.com/
//   - GET /v4/user            → infos compte (ping/test API key)
//   - GET /v4/link/unlock     → débride un lien hoster en URL directe
//
// Auth : header Authorization: Bearer <apikey> (ou query param ?apikey=).
package alldebrid

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultBase = "https://api.alldebrid.com/v4"
const userAgent = "GoPostTools/6.x"

type Client struct {
	apiKey string
	base   string
	http   *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		base:   defaultBase,
		http:   &http.Client{Timeout: 30 * time.Second},
	}
}

// envelope commune AllDebrid : {"status":"success","data":{...}} ou
// {"status":"error","error":{"code":"...","message":"..."}}
type envelope[T any] struct {
	Status string `json:"status"`
	Data   T      `json:"data"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) get(path string, params url.Values) ([]byte, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("clé API AllDebrid manquante")
	}
	u := c.base + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("alldebrid HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return body, nil
}

// User : infos compte AllDebrid.
type User struct {
	Username    string `json:"username"`
	Email       string `json:"email"`
	IsPremium   bool   `json:"isPremium"`
	PremiumUntil int64 `json:"premiumUntil"` // unix timestamp
}

// Me : retourne les infos du compte (validation API key).
func (c *Client) Me() (*User, error) {
	data, err := c.get("/user", nil)
	if err != nil {
		return nil, err
	}
	var env envelope[struct {
		User User `json:"user"`
	}]
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parse user: %w", err)
	}
	if env.Status != "success" {
		if env.Error != nil {
			return nil, fmt.Errorf("alldebrid: %s (%s)", env.Error.Message, env.Error.Code)
		}
		return nil, fmt.Errorf("alldebrid: réponse non-success")
	}
	return &env.Data.User, nil
}

// UnlockedLink : résultat d'un débridage.
type UnlockedLink struct {
	Link     string `json:"link"`     // URL directe débridée
	Filename string `json:"filename"` // nom du fichier
	Host     string `json:"host"`     // ex: "uptobox", "rapidgator"…
	Filesize int64  `json:"filesize"` // taille en bytes
	ID       string `json:"id"`
}

// Unlock débride un lien partagé en URL directe.
// Renvoie une erreur si le hoster n'est pas supporté ou si le lien est mort.
func (c *Client) Unlock(link string) (*UnlockedLink, error) {
	if strings.TrimSpace(link) == "" {
		return nil, fmt.Errorf("lien vide")
	}
	params := url.Values{}
	params.Set("link", link)
	data, err := c.get("/link/unlock", params)
	if err != nil {
		return nil, err
	}
	var env envelope[UnlockedLink]
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parse unlock: %w", err)
	}
	if env.Status != "success" {
		if env.Error != nil {
			return nil, fmt.Errorf("alldebrid unlock: %s (%s)", env.Error.Message, env.Error.Code)
		}
		return nil, fmt.Errorf("alldebrid unlock: réponse non-success")
	}
	if env.Data.Link == "" {
		return nil, fmt.Errorf("alldebrid: URL débridée vide")
	}
	return &env.Data, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
