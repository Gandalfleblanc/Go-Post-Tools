package api

import "encoding/json"

func (c *Client) Login(email, password, tokenName, token string) (*User, error) {
	payload := LoginRequest{Email: email, Password: password, TokenName: tokenName, Token: token}
	data, err := c.post("/auth/login", payload)
	if err != nil {
		return nil, err
	}
	var resp struct {
		User User `json:"user"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	if resp.User.AccessToken != "" {
		c.token = resp.User.AccessToken
	}
	return &resp.User, nil
}

func (c *Client) Register(email, password, tokenName string) (*User, error) {
	payload := RegisterRequest{Email: email, Password: password, TokenName: tokenName}
	data, err := c.post("/auth/register", payload)
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
