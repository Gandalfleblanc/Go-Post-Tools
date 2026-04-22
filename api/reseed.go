package api

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// ReseedRequest représente une demande de reseed telle que retournée par
// l'endpoint admin /reseed-requests (objet nested avec torrent / requester / uploader).
type ReseedRequest struct {
	ID          int    `json:"id"`
	TorrentID   int    `json:"torrent_id"`
	RequesterID int    `json:"requester_id"`
	UploaderID  int    `json:"uploader_id"`
	Status      string `json:"status"` // "pending" | "done" | "rejected"
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`

	Torrent   ReseedRequestTorrent `json:"torrent"`
	Requester ReseedRequestUser    `json:"requester"`
	Uploader  ReseedRequestUser    `json:"uploader"`
}

type ReseedRequestTorrent struct {
	ID          int     `json:"id"`
	TitleID     int     `json:"title_id"`
	TorrentName string  `json:"torrent_name"`
	Name        string  `json:"name"`
	InfoHash    string  `json:"info_hash"`
	Seeders     int     `json:"seeders"`
	Author      string  `json:"author"`
	Size        *int64  `json:"size"`
	Title       ReseedRequestTitle `json:"title"`
}

type ReseedRequestTitle struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Poster string `json:"poster"`
	Year   *int   `json:"year"`
}

type ReseedRequestUser struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

// ReseedRequestsResponse est la page retournée.
type ReseedRequestsResponse struct {
	Pagination struct {
		CurrentPage int             `json:"current_page"`
		LastPage    int             `json:"last_page,omitempty"`
		Total       int             `json:"total,omitempty"`
		Data        []ReseedRequest `json:"data"`
	} `json:"pagination"`
}

// ListReseedRequests appelle /reseed-requests avec filtres optionnels.
// status : "" | "pending" | "done" | "rejected"
// uploaderID / requesterID : 0 pour ignorer le filtre
// page : 1+
func (c *Client) ListReseedRequests(status string, uploaderID, requesterID, page int) (*ReseedRequestsResponse, error) {
	params := url.Values{}
	if status != "" {
		params.Set("status", status)
	}
	if uploaderID > 0 {
		params.Set("uploader_id", strconv.Itoa(uploaderID))
	}
	if requesterID > 0 {
		params.Set("requester_id", strconv.Itoa(requesterID))
	}
	if page > 0 {
		params.Set("page", strconv.Itoa(page))
	}
	params.Set("perPage", "50")

	data, err := c.get("/reseed-requests", params)
	if err != nil {
		return nil, err
	}
	var resp ReseedRequestsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse reseed-requests: %w (raw: %s)", err, truncate(string(data), 300))
	}
	return &resp, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
