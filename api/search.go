package api

import (
	"encoding/json"
	"fmt"
	"net/url"
)

type SearchResult struct {
	Query   string            `json:"query"`
	Titles  []PartialTitle    `json:"titles,omitempty"`
	People  []PartialPerson   `json:"people,omitempty"`
}

func (c *Client) Search(query string, limit int) (*SearchResult, error) {
	params := url.Values{}
	if limit > 0 {
		params.Set("limit", intParam(limit))
	}

	data, err := c.get(fmt.Sprintf("/search/%s", url.PathEscape(query)), params)
	if err != nil {
		return nil, err
	}

	var raw struct {
		Query   string            `json:"query"`
		Results []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	result := &SearchResult{Query: raw.Query}
	for _, r := range raw.Results {
		// Discriminate by presence of "type" field (titles have it, people don't)
		var probe struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(r, &probe)
		if probe.Type != "" {
			var t PartialTitle
			if err := json.Unmarshal(r, &t); err == nil {
				result.Titles = append(result.Titles, t)
			}
		} else {
			var p PartialPerson
			if err := json.Unmarshal(r, &p); err == nil {
				result.People = append(result.People, p)
			}
		}
	}
	return result, nil
}
