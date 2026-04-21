package api

import "encoding/json"

func (c *Client) GetLangs() ([]Lang, error) {
	data, err := c.get("/meta/langs", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Langs []Lang `json:"langs"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return resp.Langs, nil
}

func (c *Client) GetSubs() ([]Lang, error) {
	data, err := c.get("/meta/subs", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Subs []Lang `json:"subs"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return resp.Subs, nil
}

func (c *Client) GetQualities() ([]Quality, error) {
	data, err := c.get("/meta/quals", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Quals []Quality `json:"quals"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return resp.Quals, nil
}
