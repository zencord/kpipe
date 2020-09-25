package kpipe

import (
	"context"
	"net"
	"time"

	"k8s.io/apimachinery/pkg/util/httpstream"
)

type Connection struct {
	target       httpstream.Stream
	errorChannel chan error
	service      string
	port         int
}

func (c *Connection) Read(b []byte) (n int, err error) {
	read, err := c.target.Read(b)

	if err != nil {
		return 0, c.tryReadError(err)
	}

	return read, err
}

func (c *Connection) tryReadError(err error) error {
	ctx, _ := context.WithTimeout(context.TODO(), 3*time.Second)

	select {
	case <-ctx.Done():
		return err
	case e := <-c.errorChannel:
		return e
	}
}

func (c *Connection) Write(b []byte) (n int, err error) {
	write, err := c.target.Write(b)

	if err != nil {
		return 0, c.tryReadError(err)
	}

	return write, err
}

func (c *Connection) Close() error {
	return c.target.Close()
}

func (c *Connection) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: c.port, Zone: "LocalKube"}
}

func (c *Connection) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: c.port, Zone: "LocalKube"}
}

func (c *Connection) SetDeadline(_ time.Time) error {
	return nil
}

func (c *Connection) SetReadDeadline(_ time.Time) error {
	return nil
}

func (c *Connection) SetWriteDeadline(_ time.Time) error {
	return nil
}
