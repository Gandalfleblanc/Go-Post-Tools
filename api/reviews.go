package api

import (
	"encoding/json"
	"fmt"
)

func (c *Client) CreateReview(payload CreateReviewPayload) (*Review, error) {
	data, err := c.post("/reviews", payload)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Review Review `json:"review"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return &resp.Review, nil
}

func (c *Client) UpdateReview(id int, payload UpdateReviewPayload) (*Review, error) {
	data, err := c.put(fmt.Sprintf("/reviews/%d", id), payload)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Review Review `json:"review"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return &resp.Review, nil
}

func (c *Client) DeleteReview(id int) error {
	_, err := c.delete(fmt.Sprintf("/reviews/%d", id))
	return err
}
