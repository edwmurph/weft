package runtimebackup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/edwmurph/weft/internal/config"
)

const MetadataFile = "metadata.json"

type Options struct {
	OutputDir   string
	Reason      string
	IncludeLogs bool
	Now         func() time.Time
}

type Metadata struct {
	ID         string     `json:"id"`
	CreatedAt  string     `json:"created_at"`
	Reason     string     `json:"reason,omitempty"`
	RuntimeDir string     `json:"runtime_dir"`
	Workspace  string     `json:"workspace"`
	Path       string     `json:"path"`
	Files      []FileMeta `json:"files,omitempty"`
	Missing    []string   `json:"missing,omitempty"`
}

type FileMeta struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
}

type RestoreResult struct {
	Backup     Metadata
	PreRestore *Metadata
	Restored   []string
	Removed    []string
}

func DefaultDir(rt config.Runtime) string {
	return filepath.Join(rt.Dir, "backups")
}

func Create(rt config.Runtime, opts Options) (Metadata, error) {
	now := time.Now().UTC()
	if opts.Now != nil {
		now = opts.Now().UTC()
	}
	base := opts.OutputDir
	if strings.TrimSpace(base) == "" {
		base = DefaultDir(rt)
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return Metadata{}, err
	}
	id := uniqueID(base, now, opts.Reason)
	path := filepath.Join(base, id)
	if err := os.Mkdir(path, 0o700); err != nil {
		return Metadata{}, err
	}
	meta := Metadata{
		ID:         id,
		CreatedAt:  now.Format(time.RFC3339),
		Reason:     strings.TrimSpace(opts.Reason),
		RuntimeDir: rt.Dir,
		Workspace:  rt.Workspace,
		Path:       path,
	}
	for _, item := range backupItems(rt, opts.IncludeLogs) {
		info, err := os.Stat(item.source)
		if errors.Is(err, os.ErrNotExist) {
			if item.required {
				meta.Missing = append(meta.Missing, item.name)
			}
			continue
		}
		if err != nil {
			return Metadata{}, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		target := filepath.Join(path, item.relative)
		if err := copyFile(target, item.source, info.Mode().Perm()); err != nil {
			return Metadata{}, err
		}
		meta.Files = append(meta.Files, FileMeta{
			Name:   item.name,
			Source: item.source,
			Path:   item.relative,
			Size:   info.Size(),
		})
	}
	if err := writeMetadata(path, meta); err != nil {
		return Metadata{}, err
	}
	return meta, nil
}

func List(rt config.Runtime) ([]Metadata, error) {
	dir := DefaultDir(rt)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var backups []Metadata
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		meta, err := Load(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		backups = append(backups, meta)
	}
	sort.SliceStable(backups, func(i, j int) bool {
		return backups[i].CreatedAt > backups[j].CreatedAt
	})
	return backups, nil
}

func Resolve(rt config.Runtime, idOrPath string) (Metadata, error) {
	if strings.TrimSpace(idOrPath) == "" {
		return Metadata{}, errors.New("backup id or path is required")
	}
	candidates := []string{idOrPath}
	if !filepath.IsAbs(idOrPath) {
		candidates = append(candidates, filepath.Join(DefaultDir(rt), idOrPath))
	}
	for _, candidate := range candidates {
		meta, err := Load(candidate)
		if err == nil {
			return meta, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return Metadata{}, err
		}
	}
	return Metadata{}, fmt.Errorf("backup not found: %s", idOrPath)
}

func Load(path string) (Metadata, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Metadata{}, err
	}
	if !info.IsDir() {
		return Metadata{}, fmt.Errorf("backup path is not a directory: %s", path)
	}
	data, err := os.ReadFile(filepath.Join(path, MetadataFile))
	if err != nil {
		return Metadata{}, err
	}
	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return Metadata{}, fmt.Errorf("could not parse backup metadata: %w", err)
	}
	if meta.ID == "" {
		return Metadata{}, fmt.Errorf("backup metadata is missing id: %s", path)
	}
	meta.Path = path
	return meta, nil
}

func Restore(rt config.Runtime, backup Metadata) (RestoreResult, error) {
	pre, err := Create(rt, Options{Reason: "pre-restore " + backup.ID, IncludeLogs: true})
	if err != nil {
		return RestoreResult{Backup: backup}, fmt.Errorf("could not create pre-restore backup: %w", err)
	}
	return RestoreWithPreRestore(rt, backup, &pre)
}

func RestoreWithPreRestore(rt config.Runtime, backup Metadata, pre *Metadata) (RestoreResult, error) {
	result := RestoreResult{Backup: backup, PreRestore: pre}
	files := map[string]FileMeta{}
	for _, file := range backup.Files {
		files[file.Name] = file
	}
	for _, item := range restorableItems(rt) {
		file, ok := files[item.name]
		if !ok {
			if err := os.Remove(item.source); err == nil {
				result.Removed = append(result.Removed, item.name)
			} else if err != nil && !errors.Is(err, os.ErrNotExist) {
				return result, err
			}
			continue
		}
		source := filepath.Join(backup.Path, file.Path)
		if err := copyFile(item.source, source, 0o600); err != nil {
			return result, err
		}
		result.Restored = append(result.Restored, item.name)
	}
	return result, nil
}

type backupItem struct {
	name     string
	source   string
	relative string
	required bool
}

func backupItems(rt config.Runtime, includeLogs bool) []backupItem {
	items := []backupItem{
		{name: "config.toml", source: rt.ConfigPath, relative: "config.toml", required: true},
		{name: "state.json", source: rt.StatePath, relative: "state.json", required: true},
	}
	if includeLogs {
		items = append(items,
			backupItem{name: "weftd.log", source: filepath.Join(rt.Dir, "weftd.log"), relative: filepath.Join("logs", "weftd.log")},
			backupItem{name: "weft-client.log", source: filepath.Join(rt.Dir, "weft-client.log"), relative: filepath.Join("logs", "weft-client.log")},
		)
	}
	return items
}

func restorableItems(rt config.Runtime) []backupItem {
	return []backupItem{
		{name: "config.toml", source: rt.ConfigPath},
		{name: "state.json", source: rt.StatePath},
	}
}

func uniqueID(base string, now time.Time, reason string) string {
	prefix := now.Format("20060102T150405Z")
	if slug := slugify(reason); slug != "" {
		prefix += "-" + slug
	}
	id := prefix
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(base, id)); errors.Is(err, os.ErrNotExist) {
			return id
		}
		id = fmt.Sprintf("%s-%02d", prefix, i)
	}
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDash := false
	for _, ch := range value {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			builder.WriteRune(ch)
			lastDash = false
			continue
		}
		if !lastDash && builder.Len() > 0 {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func copyFile(target string, source string, perm os.FileMode) error {
	if perm == 0 {
		perm = 0o600
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, target)
}

func writeMetadata(path string, meta Metadata) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(path, MetadataFile), data, 0o600)
}
