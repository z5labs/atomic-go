package legacy

// SetMaxBody sets the most bytes c reads from a response body, so a test
// can reach the limit without a 2 GiB body.
func SetMaxBody(c *Client, n int64) {
	c.maxBody = n
}
