package api

import (
	"encoding/json"
	"fmt"
)

func (c *Client) GetList(id int) (*FullList, error) {
	data, err := c.get(fmt.Sprintf("/lists/%d", id), nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		List FullList `json:"list"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return &resp.List, nil
}

func (c *Client) CreateList(payload CrupdateListPayload) (*PartialList, error) {
	data, err := c.post("/lists", payload)
	if err != nil {
		return nil, err
	}
	var resp struct {
		List PartialList `json:"list"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return &resp.List, nil
}

func (c *Client) UpdateList(id int, payload CrupdateListPayload) (*PartialList, error) {
	data, err := c.put(fmt.Sprintf("/lists/%d", id), payload)
	if err != nil {
		return nil, err
	}
	var resp struct {
		List PartialList `json:"list"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return &resp.List, nil
}

func (c *Client) DeleteList(id int) error {
	_, err := c.delete(fmt.Sprintf("/lists/%d", id))
	return err
}

func (c *Client) AddToList(listID int, itemID int, itemType string) (*PartialList, error) {
	payload := ListItemPayload{ItemID: itemID, ItemType: itemType}
	data, err := c.post(fmt.Sprintf("/lists/%d/add", listID), payload)
	if err != nil {
		return nil, err
	}
	var resp struct {
		List PartialList `json:"list"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return &resp.List, nil
}

func (c *Client) RemoveFromList(listID int, itemID int, itemType string) (*PartialList, error) {
	payload := ListItemPayload{ItemID: itemID, ItemType: itemType}
	data, err := c.post(fmt.Sprintf("/lists/%d/remove", listID), payload)
	if err != nil {
		return nil, err
	}
	var resp struct {
		List PartialList `json:"list"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return &resp.List, nil
}
