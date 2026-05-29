package ptyx

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	"github.com/creack/pty"
)

type Data struct {
	TabID string
	Text  string
	Title string
	Err   error
}

type Session struct {
	TabID string
	cmd   *exec.Cmd
	file  *os.File
	mu    sync.Mutex
	text  string
}

func Start(ctx context.Context, tabID string, command string, workdir string, cols int, rows int, output func(Data)) (*Session, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.CommandContext(ctx, shell, "-lc", command)
	cmd.Dir = workdir
	cmd.Env = childEnv(os.Environ())
	file, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(max(cols, 20)), Rows: uint16(max(rows, 5))})
	if err != nil {
		return nil, err
	}
	session := &Session{TabID: tabID, cmd: cmd, file: file}
	go session.readLoop(output)
	return session, nil
}

func (s *Session) Write(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return os.ErrClosed
	}
	_, err := s.file.Write(data)
	return err
}

func (s *Session) Resize(cols int, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return
	}
	_ = pty.Setsize(s.file, &pty.Winsize{Cols: uint16(max(cols, 20)), Rows: uint16(max(rows, 5))})
}

func (s *Session) Kill() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file != nil {
		_ = s.file.Close()
		s.file = nil
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
}

func (s *Session) Text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.text
}

func (s *Session) readLoop(output func(Data)) {
	defer func() {
		if s.cmd != nil {
			_ = s.cmd.Wait()
		}
		output(Data{TabID: s.TabID, Err: os.ErrClosed})
	}()
	buf := make([]byte, 4096)
	for {
		n, err := s.file.Read(buf)
		if n > 0 {
			terminalData, response := answerColorRequests(buf[:n])
			if len(response) > 0 {
				_ = s.Write(response)
			}
			clean, title := stripOSC(terminalData)
			if len(clean) > 0 {
				s.mu.Lock()
				s.text += string(clean)
				if len(s.text) > 120000 {
					s.text = s.text[len(s.text)-90000:]
				}
				s.mu.Unlock()
			}
			output(Data{TabID: s.TabID, Text: string(clean), Title: title})
		}
		if err != nil {
			return
		}
	}
}

func childEnv(env []string) []string {
	remove := map[string]bool{
		"CODUX_HOME": true, "CODUX_WORKDIR": true, "NO_COLOR": true,
	}
	next := make([]string, 0, len(env)+1)
	hasTerm := false
	for _, item := range env {
		key := item
		if index := strings.Index(item, "="); index >= 0 {
			key = item[:index]
		}
		if remove[key] {
			continue
		}
		if key == "TERM" {
			hasTerm = true
		}
		next = append(next, item)
	}
	if !hasTerm {
		next = append(next, "TERM=xterm-256color")
	}
	return next
}

const (
	defaultForegroundResponse = "\x1b]10;rgb:eded/efef/f1f1\x1b\\"
	defaultBackgroundResponse = "\x1b]11;rgb:2828/3131/3838\x1b\\"
)

var (
	oscTitleRE        = regexp.MustCompile(`\x1b\](?:0|1|2);([^\x07\x1b]*)(?:\x07|\x1b\\)`)
	oscColorRequestRE = regexp.MustCompile(`\x1b\](10|11);\?(?:\x07|\x1b\\)`)
)

func answerColorRequests(data []byte) ([]byte, []byte) {
	var response []byte
	terminalData := oscColorRequestRE.ReplaceAllFunc(data, func(match []byte) []byte {
		parts := oscColorRequestRE.FindSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		switch string(parts[1]) {
		case "10":
			response = append(response, defaultForegroundResponse...)
			return []byte(defaultForegroundResponse)
		case "11":
			response = append(response, defaultBackgroundResponse...)
			return []byte(defaultBackgroundResponse)
		default:
			return match
		}
	})
	return terminalData, response
}

func stripOSC(data []byte) ([]byte, string) {
	title := ""
	clean := oscTitleRE.ReplaceAllFunc(data, func(match []byte) []byte {
		parts := oscTitleRE.FindSubmatch(match)
		if len(parts) > 1 {
			title = string(parts[1])
		}
		return []byte{}
	})
	return clean, title
}
