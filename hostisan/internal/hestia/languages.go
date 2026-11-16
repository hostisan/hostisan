package hestia

import "context"

// Languages returns the language codes from `v-list-sys-languages json`.
func (c *Client) Languages(ctx context.Context) ([]string, error) {
	var langs []string
	if err := c.runJSON(ctx, &langs, "v-list-sys-languages", "json"); err != nil {
		return nil, err
	}
	return langs, nil
}
