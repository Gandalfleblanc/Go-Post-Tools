package api

import "fmt"

// DeleteTorrent supprime un torrent par son ID (requiert permission torrents.delete).
func (c *Client) DeleteTorrent(id int) error {
	_, err := c.delete(fmt.Sprintf("/torrents/%d", id))
	return err
}

// DeleteNzb supprime un NZB par son ID (requiert permission nzb.delete).
func (c *Client) DeleteNzb(id int) error {
	_, err := c.delete(fmt.Sprintf("/nzb/%d", id))
	return err
}

// DeleteLien supprime un lien DDL par son ID (requiert permission liens.delete).
func (c *Client) DeleteLien(id int) error {
	_, err := c.delete(fmt.Sprintf("/liens/%d", id))
	return err
}
