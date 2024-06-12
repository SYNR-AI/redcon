package redcon

import (
	"errors"
	"io"
	"net"
	"time"
)

type ServerOptions struct {
	ConnectionCount   func(addr string, tag string)
	ConnectionLatency func(addr string, tag string, latency int64)
}

var (
	serverOptions = &ServerOptions{}
)

func WithConnectionCount(fn func(addr string, tag string)) {
	serverOptions.ConnectionCount = fn
}

func WithConnectionLatency(fn func(addr string, tag string, latency int64)) {
	serverOptions.ConnectionLatency = fn
}

func (s *Server) Listen(signal chan error) error {
	ln, err := net.Listen(s.net, s.laddr)
	if err != nil {
		if signal != nil {
			signal <- err
		}
		return err
	}
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()
	if signal != nil {
		signal <- nil
	}
	// slot: addons start
	return serveAddons(s)
	// slot: addons end
}

func serveAddons(s *Server) error {
	defer func() {
		s.ln.Close()
		func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			for c := range s.conns {
				c.conn.Close()
			}
			s.conns = nil
		}()
	}()
	for {
		// slot: addons start
		start := time.Now()
		latency := int64(0)
		// slot: addons end

		lnconn, err := s.ln.Accept()
		if err != nil {
			s.mu.Lock()
			done := s.done
			s.mu.Unlock()
			if done {
				return nil
			}
			if errors.Is(err, net.ErrClosed) {
				// see https://github.com/tidwall/redcon/issues/46
				return nil
			}
			if s.AcceptError != nil {
				s.AcceptError(err)
			}
			continue
		}

		// slot: addons start
		defer func() {
			addr := lnconn.RemoteAddr().String()
			if serverOptions.ConnectionLatency != nil {
				serverOptions.ConnectionCount(addr, "accept")
			}
			if serverOptions.ConnectionLatency != nil {
				serverOptions.ConnectionLatency(addr, "accept", latency)
			}
		}()
		// slot: addons end

		c := &conn{
			conn: lnconn,
			addr: lnconn.RemoteAddr().String(),
			wr:   NewWriter(lnconn),
			rd:   NewReader(lnconn),
		}
		s.mu.Lock()
		c.idleClose = s.idleClose
		s.conns[c] = true
		s.mu.Unlock()
		if s.accept != nil && !s.accept(c) {
			s.mu.Lock()
			delete(s.conns, c)
			s.mu.Unlock()
			c.Close()
			continue
		}

		// slot: addons start
		latency = time.Now().Sub(start).Microseconds()
		// slot: addons end

		go handle(s, c)
	}
}

// handleAddons manages the server connection.
func handleAddons(s *Server, c *conn) {
	var err error
	defer func() {
		if err != errDetached {
			// do not close the connection when a detach is detected.
			c.conn.Close()
		}
		func() {
			// slot: addons start
			start := time.Now()
			latency := int64(0)
			defer func() {
				if serverOptions.ConnectionCount != nil {
					serverOptions.ConnectionCount(c.addr, "closed")
				}
				if serverOptions.ConnectionLatency != nil {
					serverOptions.ConnectionLatency(c.addr, "closed", latency)
				}
			}()
			// slot: addons end

			// remove the conn from the server
			s.mu.Lock()
			defer s.mu.Unlock()
			delete(s.conns, c)
			if s.closed != nil {
				if err == io.EOF {
					err = nil
				}
				s.closed(c, err)

				// slot: addons start
				latency = time.Now().Sub(start).Microseconds()
				// slot: addons end
			}
		}()
	}()

	err = func() error {
		// read commands and feed back to the client
		for {
			// read pipeline commands
			if c.idleClose != 0 {
				c.conn.SetReadDeadline(time.Now().Add(c.idleClose))
			}
			cmds, err := c.rd.readCommands(nil)
			if err != nil {
				if err, ok := err.(*errProtocol); ok {
					// All protocol errors should attempt a response to
					// the client. Ignore write errors.
					c.wr.WriteError("ERR " + err.Error())
					c.wr.Flush()
				}
				return err
			}
			c.cmds = cmds
			for len(c.cmds) > 0 {
				cmd := c.cmds[0]
				if len(c.cmds) == 1 {
					c.cmds = nil
				} else {
					c.cmds = c.cmds[1:]
				}
				s.handler(c, cmd)
			}
			if c.detached {
				// client has been detached
				return errDetached
			}
			if c.closed {
				return nil
			}
			if err := c.wr.Flush(); err != nil {
				return err
			}
		}
	}()
}
