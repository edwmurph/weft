package taskcontext

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/edwmurph/weft/internal/filex"
)

const (
	FileName        = "task-context.json"
	Version         = 2
	MaxHeadingBytes = 512
	MaxDetailBytes  = 16 * 1024
)

type Record struct {
	TaskID    string `json:"task_id"`
	Heading   string `json:"heading,omitempty"`
	Detail    string `json:"detail,omitempty"`
	UpdatedAt string `json:"updated_at"`
}

type File struct {
	Version int               `json:"version"`
	Records map[string]Record `json:"records"`
}

type Store struct {
	Path     string
	LockPath string
}

func NewStore(runtimeDir string) *Store {
	path := filepath.Join(runtimeDir, FileName)
	return &Store{Path: path, LockPath: path + ".lock"}
}

func Summary(content string) string {
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if line != "" {
			return line
		}
	}
	return ""
}

func (r Record) Summary() string {
	if strings.TrimSpace(r.Heading) != "" {
		return strings.Join(strings.Fields(r.Heading), " ")
	}
	return Summary(r.Detail)
}

func (r Record) HasContent() bool {
	return strings.TrimSpace(r.Heading) != "" || strings.TrimSpace(r.Detail) != ""
}

func (s *Store) Load() (map[string]Record, error) {
	if err := filex.EnsureLockFile(s.LockPath); err != nil {
		return nil, err
	}
	records := map[string]Record{}
	err := filex.WithFileLock(s.LockPath, func() error {
		loaded, err := s.readUnlocked()
		if err != nil {
			return err
		}
		records = loaded
		return nil
	})
	return records, err
}

func (s *Store) SetHeading(taskID string, content string) (Record, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return Record{}, errors.New("task id is required")
	}
	content, err := validateHeading(content)
	if err != nil {
		return Record{}, err
	}
	if err := filex.EnsureLockFile(s.LockPath); err != nil {
		return Record{}, err
	}
	var record Record
	now := nowISO()
	err = filex.WithFileLock(s.LockPath, func() error {
		records, err := s.readUnlocked()
		if err != nil {
			return err
		}
		record = records[taskID]
		record.TaskID = taskID
		record.Heading = content
		record.UpdatedAt = now
		records[taskID] = record
		return s.writeUnlocked(records)
	})
	return record, err
}

func (s *Store) SetDetail(taskID string, content string) (Record, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return Record{}, errors.New("task id is required")
	}
	content, err := validateDetail(content)
	if err != nil {
		return Record{}, err
	}
	if err := filex.EnsureLockFile(s.LockPath); err != nil {
		return Record{}, err
	}
	var record Record
	now := nowISO()
	err = filex.WithFileLock(s.LockPath, func() error {
		records, err := s.readUnlocked()
		if err != nil {
			return err
		}
		record = records[taskID]
		record.TaskID = taskID
		record.Detail = content
		record.UpdatedAt = now
		records[taskID] = record
		return s.writeUnlocked(records)
	})
	return record, err
}

func (s *Store) Show(taskID string) (Record, bool, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return Record{}, false, errors.New("task id is required")
	}
	records, err := s.Load()
	if err != nil {
		return Record{}, false, err
	}
	record, ok := records[taskID]
	return record, ok, nil
}

func (s *Store) Clear(taskID string, kind string) (bool, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return false, errors.New("task id is required")
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		kind = "all"
	}
	switch kind {
	case "heading", "detail", "all":
	default:
		return false, fmt.Errorf("unsupported task context kind %q", kind)
	}
	if err := filex.EnsureLockFile(s.LockPath); err != nil {
		return false, err
	}
	removed := false
	err := filex.WithFileLock(s.LockPath, func() error {
		records, err := s.readUnlocked()
		if err != nil {
			return err
		}
		record, ok := records[taskID]
		if !ok {
			return s.writeUnlocked(records)
		}
		switch kind {
		case "heading":
			removed = strings.TrimSpace(record.Heading) != ""
			record.Heading = ""
		case "detail":
			removed = strings.TrimSpace(record.Detail) != ""
			record.Detail = ""
		case "all":
			removed = record.HasContent()
			record.Heading = ""
			record.Detail = ""
		}
		if record.HasContent() {
			record.UpdatedAt = nowISO()
			records[taskID] = record
		} else {
			delete(records, taskID)
		}
		return s.writeUnlocked(records)
	})
	return removed, err
}

func (s *Store) Cleanup(validTaskIDs map[string]bool) (int, error) {
	if validTaskIDs == nil {
		validTaskIDs = map[string]bool{}
	}
	if err := filex.EnsureLockFile(s.LockPath); err != nil {
		return 0, err
	}
	removed := 0
	err := filex.WithFileLock(s.LockPath, func() error {
		records, err := s.readUnlocked()
		if err != nil {
			return err
		}
		for taskID := range records {
			if !validTaskIDs[taskID] {
				delete(records, taskID)
				removed++
			}
		}
		if removed == 0 {
			return nil
		}
		return s.writeUnlocked(records)
	})
	return removed, err
}

func validateHeading(content string) (string, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.TrimSpace(content)
	if content == "" {
		return "", errors.New("task heading context cannot be empty")
	}
	if strings.Contains(content, "\n") {
		return "", errors.New("task heading context must be one line")
	}
	content = strings.Join(strings.Fields(content), " ")
	if len([]byte(content)) > MaxHeadingBytes {
		return "", fmt.Errorf("task heading context is too large: maximum is %d bytes", MaxHeadingBytes)
	}
	return content, nil
}

func validateDetail(content string) (string, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.TrimSpace(content)
	if content == "" {
		return "", errors.New("task detail context cannot be empty")
	}
	if len([]byte(content)) > MaxDetailBytes {
		return "", fmt.Errorf("task detail context is too large: maximum is %d bytes", MaxDetailBytes)
	}
	return content, nil
}

func (s *Store) readUnlocked() (map[string]Record, error) {
	raw, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]Record{}, nil
	}
	if err != nil {
		return nil, err
	}
	var file File
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return nil, fmt.Errorf("could not parse %s: %w", FileName, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return nil, fmt.Errorf("could not parse %s: multiple JSON values", FileName)
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("could not parse %s: %w", FileName, err)
	}
	if file.Version != Version {
		return nil, fmt.Errorf("unsupported %s version %d", FileName, file.Version)
	}
	records := map[string]Record{}
	for id, record := range file.Records {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, fmt.Errorf("%s contains an empty task id", FileName)
		}
		if strings.TrimSpace(record.TaskID) == "" {
			record.TaskID = id
		}
		if record.TaskID != id {
			return nil, fmt.Errorf("%s record key %q does not match task_id %q", FileName, id, record.TaskID)
		}
		record.Heading = strings.TrimSpace(record.Heading)
		record.Detail = strings.TrimSpace(strings.ReplaceAll(record.Detail, "\r\n", "\n"))
		if record.Heading != "" {
			heading, err := validateHeading(record.Heading)
			if err != nil {
				return nil, fmt.Errorf("%s record %q is invalid: %w", FileName, id, err)
			}
			record.Heading = heading
		}
		if record.Detail != "" {
			detail, err := validateDetail(record.Detail)
			if err != nil {
				return nil, fmt.Errorf("%s record %q is invalid: %w", FileName, id, err)
			}
			record.Detail = detail
		}
		if !record.HasContent() {
			return nil, fmt.Errorf("%s record %q is invalid: task context cannot be empty", FileName, id)
		}
		records[id] = record
	}
	return records, nil
}

func (s *Store) writeUnlocked(records map[string]Record) error {
	if len(records) == 0 {
		if err := os.Remove(s.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	ordered := make(map[string]Record, len(records))
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		record := records[key]
		if record.HasContent() {
			ordered[key] = record
		}
	}
	if len(ordered) == 0 {
		if err := os.Remove(s.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return filex.WriteJSONAtomic(s.Path, File{Version: Version, Records: ordered})
}

func nowISO() string {
	return time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
}
