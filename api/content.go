package api

import (
	"encoding/json"
	"fmt"
	"net/url"
)

type TorrentsResult struct {
	Torrents    []TorrentItem `json:"torrents"`
	Count       int           `json:"count"`
	Charged     float64       `json:"charged"`
	AlreadyPaid int           `json:"already_paid"`
}

type NzbsResult struct {
	Nzbs        []Nzb   `json:"nzbs"`
	Count       int     `json:"count"`
	Charged     float64 `json:"charged"`
	AlreadyPaid int     `json:"already_paid"`
}

type LiensResult struct {
	Liens       []Lien  `json:"liens"`
	Count       int     `json:"count"`
	Charged     float64 `json:"charged"`
	AlreadyPaid int     `json:"already_paid"`
}

func contentParams(f ContentFilter) url.Values {
	params := url.Values{}
	if f.Lang > 0 {
		params.Set("lang", intParam(f.Lang))
	}
	if f.Quality > 0 {
		params.Set("qual", intParam(f.Quality))
	}
	if f.Episode > 0 {
		params.Set("episode", intParam(f.Episode))
	}
	if f.Season > 0 {
		params.Set("saison", intParam(f.Season))
	}
	if f.Limit > 0 {
		params.Set("limit", intParam(f.Limit))
	}
	return params
}

func (c *Client) GetTorrents(titleID int, f ContentFilter) (*TorrentsResult, error) {
	data, err := c.get(fmt.Sprintf("/titles/%d/content/torrents", titleID), contentParams(f))
	if err != nil {
		return nil, err
	}
	LastRawTorrents = string(data)
	var resp struct {
		Data TorrentsResult `json:"data"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

var LastRawTorrents string

// GetLienByID appelle /content/liens/{id} et retourne le Lien complet avec
// l'URL de partage réelle (les endpoints de liste ne la renvoient pas).
func (c *Client) GetLienByID(id int) (*Lien, error) {
	data, err := c.get(fmt.Sprintf("/content/liens/%d", id), nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data struct {
			Lien Lien `json:"lien"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return &resp.Data.Lien, nil
}

// GetTorrentByID appelle /content/torrents/{id} et retourne l'item complet
// avec le download_url réel (les endpoints de liste ne le renvoient pas).
func (c *Client) GetTorrentByID(id int) (*TorrentItem, error) {
	data, err := c.get(fmt.Sprintf("/content/torrents/%d", id), nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data struct {
			Torrent TorrentItem `json:"torrent"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return &resp.Data.Torrent, nil
}

func (c *Client) GetNzbs(titleID int, f ContentFilter) (*NzbsResult, error) {
	data, err := c.get(fmt.Sprintf("/titles/%d/content/nzbs", titleID), contentParams(f))
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data NzbsResult `json:"data"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) GetLiens(titleID int, f ContentFilter) (*LiensResult, error) {
	data, err := c.get(fmt.Sprintf("/titles/%d/content/liens", titleID), contentParams(f))
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data LiensResult `json:"data"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}
