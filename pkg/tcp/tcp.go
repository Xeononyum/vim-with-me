package tcp

import (
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
)

var VERSION = 1

type TCPStream struct {
	outs []chan TCPCommand
	lock sync.RWMutex
}

func (t *TCPStream) Spread(command TCPCommand) {
	t.lock.RLock()
	for _, listener := range t.outs {
		listener <- command
	}
	t.lock.RUnlock()
}

func (t *TCPStream) listen() <-chan TCPCommand {
	t.lock.Lock()
	listener := make(chan TCPCommand, 10)
	t.outs = append(t.outs, listener)
	t.lock.Unlock()
	return listener
}

func (t *TCPStream) removeListen(rm <-chan TCPCommand) {
	t.lock.Lock()
	for i, listener := range t.outs {
		if listener == rm {
			t.outs = append(t.outs[:i], t.outs[i+1:]...)
			break
		}
	}
	t.lock.Unlock()
}

func createTCPCommandSpread() TCPStream {
	return TCPStream{
		outs: make([]chan TCPCommand, 0),
		lock: sync.RWMutex{},
	}
}

type TCPCommand struct {
	Command string
	Data    string
}

var malformedTCPCommand = TCPCommand{
	Command: "e",
	Data:    "Malformed TCP Command",
}

func versionMismatch(v1, v2 int) *TCPCommand {
    return &TCPCommand{
        Command: "e",
        Data:    fmt.Sprintf("Version Mismatch %d %d", v1, v2),
    }
}

var tcpClosedCommand = TCPCommand{
	Command: "c",
	Data:    "Connection Closed",
}

func (t *TCPCommand) Bytes() []byte {
    str := fmt.Sprintf("%s:%s", t.Command, t.Data)
    str = fmt.Sprintf("%d:%d:%s", VERSION, len(str), str)
	return []byte(str)
}

func CommandFromBytes(b string) (string, *TCPCommand) {
	parts := strings.SplitN(b, ":", 3)
	if len(parts) != 3 {
		return b, nil
	}

    versionStr := parts[0]
    lengthStr := parts[1]
    dataStr := parts[2]

	version, err := strconv.Atoi(versionStr)
	if err != nil {
		return b, &malformedTCPCommand
	}

    if version != VERSION {
        return b, versionMismatch(version, VERSION)
    }

    length, err := strconv.Atoi(lengthStr)
	if err != nil {
		return b, &malformedTCPCommand
	}
	if len(dataStr) < length {
		return b, nil
	}

	remaining := dataStr[length:]
	commandStr := dataStr[:length]
	commandParts := strings.SplitN(commandStr, ":", 2)

	if len(commandParts) != 2 {
		return b, &malformedTCPCommand
	}

	cmd := &TCPCommand{
		Command: commandParts[0],
		Data:    commandParts[1],
	}

	return remaining, cmd
}

type TCP struct {
	FromSockets chan TCPCommand
	ToSockets   TCPStream
}

func (t *TCP) Send(cmd TCPCommand) {
	t.ToSockets.Spread(cmd)
}

func CommandParser(r io.Reader) chan TCPCommand {
	out := make(chan TCPCommand)

	go func() {
        defer close(out)

		buffer := make([]byte, 1024)
		previous := ""
		for {
			n, err := r.Read(buffer)
			if err != nil {
                out <- tcpClosedCommand
				return
			}
			current := previous + string(buffer[:n])

			for remaining, cmd := CommandFromBytes(current); cmd != nil; remaining, cmd = CommandFromBytes(current) {
				current = remaining
				out <- *cmd
			}

			previous = current
		}
	}()

	return out
}

func (t *TCP) listen(listener net.Listener) {
	defer listener.Close()

	for {

		conn, err := listener.Accept()
		if err != nil {
			// true and factual
			log.Fatal("You like amouranth", err)
		}

		toTcp := t.ToSockets.listen()
		cmds := CommandParser(conn)

		go func(c net.Conn) {

            defer t.ToSockets.removeListen(toTcp)
			defer c.Close()

		OuterLoop:
			for {
				select {
				case cmd := <-toTcp:
					_, err := c.Write(cmd.Bytes())
					if err != nil {
						fmt.Printf("Error writing to client: %s\n", err)
						break OuterLoop
					}
				case cmd := <-cmds:
                    // NOTE: i am sure there is a better way to do this
                    // TODO: Figure out that better way
					if cmd.Command == "c" {
						break OuterLoop
					}

					t.FromSockets <- cmd

					if cmd.Command == "e" {
						break OuterLoop
					}
				}
			}

		}(conn)
	}
}

func NewTCPServer(port uint16) (*TCP, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, fmt.Errorf("Error creating TCP server: %w", err)
	}

	tcps := &TCP{
		FromSockets: make(chan TCPCommand, 10),
		ToSockets:   createTCPCommandSpread(),
	}

	go func() { tcps.listen(listener) }()

	return tcps, nil

}
