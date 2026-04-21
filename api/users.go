package api

import (
	"encoding/json"
	"fmt"
	"net/url"
)

func (c *Client) GetUser(id string) (*User, error) {
	data, err := c.get("/user-profile/"+id, nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		User User `json:"user"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return &resp.User, nil
}

func (c *Client) GetMe() (*User, error) {
	return c.GetUser("me")
}

func (c *Client) GetUserLists(id string, page, perPage int) ([]PartialList, error) {
	params := url.Values{}
	if page > 0 {
		params.Set("page", intParam(page))
	}
	if perPage > 0 {
		params.Set("perPage", intParam(perPage))
	}
	data, err := c.get(fmt.Sprintf("/user-profile/%s/lists", id), params)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Pagination struct {
			Data []PartialList `json:"data"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return resp.Pagination.Data, nil
}
