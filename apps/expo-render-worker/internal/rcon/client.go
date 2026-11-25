package rcon

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	gorcon "github.com/gorcon/rcon"
)

type Client struct {
	addr     string
	password string
	timeout  time.Duration
	mu       sync.Mutex
	conn     *gorcon.Conn
	rawConn  net.Conn
}

func NewClient(addr, password string, timeout time.Duration) *Client {
	return &Client{addr: addr, password: password, timeout: timeout}
}

func (c *Client) Execute(ctx context.Context, command string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}

	if c.conn == nil {
		dialer := net.Dialer{Timeout: c.timeout}
		conn, err := dialer.DialContext(ctx, "tcp", c.addr)
		if err != nil {
			return "", fmt.Errorf("connect to RCON: %w", err)
		}
		c.conn, err = gorcon.Open(conn, c.password, gorcon.SetDeadline(c.timeout))
		if err != nil {
			_ = conn.Close()
			return "", fmt.Errorf("authenticate RCON: %w", err)
		}
		c.rawConn = conn
	}

	raw := c.rawConn
	cancelDone := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = raw.Close()
		close(cancelDone)
	})
	response, err := c.conn.Execute(command)
	if !stop() {
		<-cancelDone
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		_ = c.conn.Close()
		c.conn = nil
		c.rawConn = nil
		return "", fmt.Errorf("execute RCON command: %w", err)
	}
	return response, nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	c.rawConn = nil
	return err
}
