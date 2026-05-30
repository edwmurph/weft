package ipc

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/edwmurph/weft/internal/state"
	"github.com/edwmurph/weft/internal/version"
)

const ProtocolVersion = 1

type Request struct {
	ProtocolVersion int               `json:"protocol_version,omitempty"`
	ClientVersion   string            `json:"client_version,omitempty"`
	Command         string            `json:"command"`
	Args            map[string]string `json:"args,omitempty"`
}

type Response struct {
	OK                bool         `json:"ok"`
	Message           string       `json:"message,omitempty"`
	Error             *Error       `json:"error,omitempty"`
	State             *state.State `json:"state,omitempty"`
	Snapshot          *Snapshot    `json:"snapshot,omitempty"`
	Upgrade           *Upgrade     `json:"upgrade,omitempty"`
	ProtocolVersion   int          `json:"protocol_version,omitempty"`
	SupervisorVersion string       `json:"supervisor_version,omitempty"`
}

type Upgrade struct {
	ClientVersion     string `json:"client_version"`
	SupervisorVersion string `json:"supervisor_version"`
	Compatible        bool   `json:"compatible"`
	RestartRequired   bool   `json:"restart_required"`
	AutoRestarted     bool   `json:"auto_restarted,omitempty"`
	RunningAgents     int    `json:"running_agents"`
	Message           string `json:"message,omitempty"`
}

type Snapshot struct {
	State          state.State `json:"state"`
	CodexTitle     string      `json:"codex_title,omitempty"`
	CodexContent   string      `json:"codex_content,omitempty"`
	LoadingText    string      `json:"loading_text,omitempty"`
	Message        string      `json:"message,omitempty"`
	NavWidth       int         `json:"nav_width"`
	GroupCursor    int         `json:"group_cursor"`
	ActiveClientID string      `json:"active_client_id,omitempty"`
	DetachClient   bool        `json:"detach_client,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e Error) Error() string {
	if e.Message == "" {
		return e.Code
	}
	return e.Message
}

func ErrorResponse(code string, message string) Response {
	return Response{OK: false, Message: message, Error: &Error{Code: code, Message: message}}
}

func Serve(path string, handler func(Request) Response) (func() error, error) {
	_ = os.Remove(path)
	listener, err := listenUnix(path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		listener.Close()
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handleConn(conn, handler)
		}
	}()
	return func() error {
		err := listener.Close()
		<-done
		_ = os.Remove(path)
		return err
	}, nil
}

func Call(path string, request Request, timeout time.Duration) (Response, error) {
	conn, err := dialUnix(path, timeout)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if request.ProtocolVersion == 0 {
		request.ProtocolVersion = ProtocolVersion
	}
	if request.ClientVersion == "" {
		request.ClientVersion = version.Version
	}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return Response{}, err
	}
	var response Response
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return Response{}, err
	}
	if !response.OK {
		if response.Error != nil {
			if response.Message == "" {
				response.Message = response.Error.Message
			}
			return response, *response.Error
		}
		if response.Message == "" {
			response.Message = "request failed"
		}
		return response, errors.New(response.Message)
	}
	return response, nil
}

func listenUnix(path string) (net.Listener, error) {
	var listener net.Listener
	err := withSocketDir(path, func(name string) error {
		var err error
		listener, err = net.Listen("unix", name)
		return err
	})
	return listener, err
}

func dialUnix(path string, timeout time.Duration) (net.Conn, error) {
	var conn net.Conn
	err := withSocketDir(path, func(name string) error {
		var err error
		conn, err = net.DialTimeout("unix", name, timeout)
		return err
	})
	return conn, err
}

func withSocketDir(path string, fn func(name string) error) error {
	dir := filepath.Dir(path)
	name := filepath.Base(path)
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := os.Chdir(dir); err != nil {
		return err
	}
	defer os.Chdir(wd)
	return fn(name)
}

func handleConn(conn net.Conn, handler func(Request) Response) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	var request Request
	if err := json.NewDecoder(reader).Decode(&request); err != nil {
		_ = json.NewEncoder(conn).Encode(Response{OK: false, Message: fmt.Sprintf("invalid request: %v", err)})
		return
	}
	_ = json.NewEncoder(conn).Encode(handler(request))
}
