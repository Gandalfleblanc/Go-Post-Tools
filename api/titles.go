package api

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

type TitlesResponse struct {
	Pagination
	Data []PartialTitle `json:"data"`
}

func (c *Client) GetTitleByTmdbID(tmdbID int) (*PartialTitle, error) {
	params := url.Values{}
	params.Set("tmdb_id", strconv.Itoa(tmdbID))
	data, err := c.get("/titles", params)
	if err != nil {
		return nil, err
	}
	// Try flat array first: {"data": [...]}
	var flat struct {
		Data []PartialTitle `json:"data"`
	}
	if err := json.Unmarshal(data, &flat); err == nil && len(flat.Data) > 0 {
		return &flat.Data[0], nil
	}
	// Try paginated: {"pagination": {"data": [...]}}
	var paged struct {
		Pagination struct {
			Data []PartialTitle `json:"data"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(data, &paged); err == nil && len(paged.Pagination.Data) > 0 {
		return &paged.Pagination.Data[0], nil
	}
	// Try direct array
	var arr []PartialTitle
	if err := json.Unmarshal(data, &arr); err == nil && len(arr) > 0 {
		return &arr[0], nil
	}
	return nil, nil
}

func (c *Client) GetTitles(f TitleFilter) (*TitlesResponse, error) {
	params := url.Values{}
	if f.PerPage > 0 {
		params.Set("perPage", intParam(f.PerPage))
	}
	if f.Page > 0 {
		params.Set("page", intParam(f.Page))
	}
	if f.Order != "" {
		params.Set("order", f.Order)
	}
	if f.Type != "" {
		params.Set("type", f.Type)
	}
	if f.Genre != "" {
		params.Set("genre", f.Genre)
	}
	if f.Language != "" {
		params.Set("language", f.Language)
	}
	if f.Score != "" {
		params.Set("score", f.Score)
	}
	if f.Released != "" {
		params.Set("released", f.Released)
	}
	if f.Country != "" {
		params.Set("country", f.Country)
	}
	if f.OnlyStreamable {
		params.Set("onlyStreamable", "true")
	}
	if f.IncludeAdult {
		params.Set("includeAdult", "true")
	}
	if f.ImdbID != "" {
		params.Set("imdb_id", f.ImdbID)
	}
	if f.TmdbID > 0 {
		params.Set("tmdb_id", strconv.Itoa(f.TmdbID))
	}

	data, err := c.get("/titles", params)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Pagination struct {
			Pagination
			Data []PartialTitle `json:"data"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return &TitlesResponse{
		Pagination: resp.Pagination.Pagination,
		Data:       resp.Pagination.Data,
	}, nil
}

func (c *Client) GetTitle(id int, seasonNumber, episodeNumber int, fullCredits bool) (*FullTitle, error) {
	params := url.Values{}
	if seasonNumber > 0 {
		params.Set("seasonNumber", intParam(seasonNumber))
	}
	if episodeNumber > 0 {
		params.Set("episodeNumber", intParam(episodeNumber))
	}
	if fullCredits {
		params.Set("fullCredits", "true")
	}

	data, err := c.get(fmt.Sprintf("/titles/%d", id), params)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Title FullTitle `json:"title"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return &resp.Title, nil
}

func (c *Client) GetRelatedTitles(id int) ([]PartialTitle, error) {
	data, err := c.get(fmt.Sprintf("/titles/%d/related", id), nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Titles []PartialTitle `json:"titles"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return resp.Titles, nil
}
