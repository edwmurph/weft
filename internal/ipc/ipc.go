package ipc

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/edwmurph/weft/internal/state"
	"github.com/edwmurph/weft/internal/version"
)

const ProtocolVersion = 5

const (
	UpgradeReasonVersion = "version"
	UpgradeReasonConfig  = "config"
)

type Request struct {
	ProtocolVersion  int               `json:"protocol_version,omitempty"`
	ClientVersion    string            `json:"client_version,omitempty"`
	ClientID         string            `json:"client_id,omitempty"`
	Width            int               `json:"width,omitempty"`
	Height           int               `json:"height,omitempty"`
	LaunchWorkspace  string            `json:"launch_workspace,omitempty"`
	ClientExecutable string            `json:"client_executable,omitempty"`
	Command          string            `json:"command"`
	Args             map[string]string `json:"args,omitempty"`
}

type Response struct {
	OK                bool         `json:"ok"`
	Message           string       `json:"message,omitempty"`
	Error             *Error       `json:"error,omitempty"`
	State             *state.State `json:"state,omitempty"`
	Snapshot          *Snapshot    `json:"snapshot,omitempty"`
	TaskContext       *TaskContext `json:"task_context,omitempty"`
	Upgrade           *Upgrade     `json:"upgrade,omitempty"`
	ProtocolVersion   int          `json:"protocol_version,omitempty"`
	SupervisorVersion string       `json:"supervisor_version,omitempty"`
	ConfigFingerprint string       `json:"config_fingerprint,omitempty"`
}

type Upgrade struct {
	ClientVersion     string `json:"client_version"`
	SupervisorVersion string `json:"supervisor_version"`
	Reason            string `json:"reason,omitempty"`
	Compatible        bool   `json:"compatible"`
	RestartRequired   bool   `json:"restart_required"`
	AutoRestarted     bool   `json:"auto_restarted,omitempty"`
	RunningTasks      int    `json:"running_tasks"`
	Message           string `json:"message,omitempty"`
	BackupID          string `json:"backup_id,omitempty"`
}

type Snapshot struct {
	State                       state.State          `json:"state"`
	LiveTitle                   string               `json:"live_title,omitempty"`
	ActiveTaskContext           *TaskContext         `json:"active_task_context,omitempty"`
	CodexContent                string               `json:"codex_content,omitempty"`
	CodexPlainLines             []string             `json:"codex_plain_lines,omitempty"`
	CodexScrollback             string               `json:"codex_scrollback,omitempty"`
	CodexScrollbackLines        []string             `json:"codex_scrollback_lines,omitempty"`
	ActiveTaskInAlternateScreen bool                 `json:"active_task_in_alternate_screen,omitempty"`
	LoadingText                 string               `json:"loading_text,omitempty"`
	LoadingTaskIDs              []string             `json:"loading_task_ids,omitempty"`
	TerminalForegroundTaskIDs   []string             `json:"terminal_foreground_task_ids,omitempty"`
	TaskOperationStartedAt      map[string]time.Time `json:"task_operation_started_at,omitempty"`
	Message                     string               `json:"message,omitempty"`
	NavWidth                    int                  `json:"nav_width"`
	GroupCursor                 int                  `json:"group_cursor"`
	ActiveClientID              string               `json:"active_client_id,omitempty"`
	ActiveClientVersion         string               `json:"active_client_version,omitempty"`
	DetachClient                bool                 `json:"detach_client,omitempty"`
}

type TaskContext struct {
	TaskID    string `json:"task_id"`
	Heading   string `json:"heading,omitempty"`
	Detail    string `json:"detail,omitempty"`
	Summary   string `json:"summary,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
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

func AnnotateUpgrade(response Response, clientVersion string, autoRestarted bool) Response {
	if response.SupervisorVersion == "" {
		return response
	}
	upgrade := UpgradeStatus(response, clientVersion)
	if upgrade == nil {
		return response
	}
	upgrade.AutoRestarted = autoRestarted
	if autoRestarted {
		upgrade.RestartRequired = false
		upgrade.Message = "Supervisor restarted on the new Weft version."
	}
	response.Upgrade = upgrade
	return response
}

func UpgradeStatus(response Response, clientVersion string) *Upgrade {
	supervisorVersion := response.SupervisorVersion
	clientVersion = strings.TrimSpace(clientVersion)
	if clientVersion == "" {
		clientVersion = version.Version
	}
	if supervisorVersion == "" || supervisorVersion == clientVersion {
		return nil
	}
	running := RunningTaskCount(responseState(response))
	compatible := response.ProtocolVersion == ProtocolVersion
	message := upgradeMessage(supervisorVersion, clientVersion, running)
	if !compatible {
		message = incompatibleUpgradeMessage(supervisorVersion, clientVersion, running)
	}
	return &Upgrade{
		ClientVersion:     clientVersion,
		SupervisorVersion: supervisorVersion,
		Reason:            UpgradeReasonVersion,
		Compatible:        compatible,
		RestartRequired:   true,
		RunningTasks:      running,
		Message:           message,
	}
}

func ShouldAutoRestart(response Response) bool {
	return response.Upgrade != nil &&
		responseState(response) != nil &&
		response.Upgrade.Compatible &&
		response.Upgrade.RestartRequired &&
		response.Upgrade.RunningTasks == 0
}

func RunningTaskCount(st *state.State) int {
	if st == nil {
		return 0
	}
	count := 0
	for _, task := range st.Tasks {
		switch task.Status {
		case state.StatusStarting, state.StatusRunning, state.StatusReady, state.StatusSitting, state.StatusShipping:
			count++
		}
	}
	return count
}

func responseState(response Response) *state.State {
	if response.State != nil {
		return response.State
	}
	if response.Snapshot != nil {
		return &response.Snapshot.State
	}
	return nil
}

func upgradeMessage(supervisorVersion string, clientVersion string, runningTasks int) string {
	if runningTasks == 0 {
		return fmt.Sprintf("Upgrade ready: client %s is newer than idle supervisor %s; the supervisor can restart safely.", clientVersion, supervisorVersion)
	}
	return fmt.Sprintf("Upgrade pending: client %s is newer than supervisor %s. Reopening the dashboard is not enough; wait for live tasks to finish or become resumable. %d live task terminal(s) are open.", clientVersion, supervisorVersion, runningTasks)
}

func incompatibleUpgradeMessage(supervisorVersion string, clientVersion string, runningTasks int) string {
	if runningTasks == 0 {
		return fmt.Sprintf("Weft client %s found incompatible supervisor %s. Saved layout and metadata remain.", clientVersion, supervisorVersion)
	}
	return fmt.Sprintf("Weft client %s found incompatible supervisor %s. Restarting the supervisor will stop %d live task terminal(s). Saved layout and metadata remain.", clientVersion, supervisorVersion, runningTasks)
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
		request.ClientVersion = reportedClientVersion()
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

func reportedClientVersion() string {
	if override := strings.TrimSpace(os.Getenv("WEFT_CLIENT_VERSION_OVERRIDE")); override != "" {
		return override
	}
	return version.Version
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
