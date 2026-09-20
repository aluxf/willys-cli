package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
)

type Object = map[string]any

func text(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
func obj(v any) Object {
	if m, ok := v.(map[string]any); ok && m != nil {
		return m
	}
	return Object{}
}
func list(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	return nil
}
func yes(v any) bool { b, _ := v.(bool); return b }
func number(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}
func first(values ...any) any {
	for _, v := range values {
		if v != nil && text(v) != "" {
			return v
		}
	}
	return nil
}
func clone(m Object) Object {
	out := Object{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func DataRoot() (string, error) {
	if p := os.Getenv("WILLYS_DATA_DIR"); p != "" {
		return filepath.Abs(p)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "willys"), nil
	case "windows":
		if p := os.Getenv("LOCALAPPDATA"); p != "" {
			return filepath.Join(p, "willys"), nil
		}
		return filepath.Join(home, "AppData", "Local", "willys"), nil
	default:
		if p := os.Getenv("XDG_DATA_HOME"); p != "" {
			return filepath.Join(p, "willys"), nil
		}
		return filepath.Join(home, ".local", "share", "willys"), nil
	}
}

type Profile struct{ Path string }

var profileName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func NewProfile(root, name string) (*Profile, error) {
	if !profileName.MatchString(name) {
		return nil, errors.New("profile names allow letters, numbers, underscores, and hyphens")
	}
	path := filepath.Join(root, name)
	if err := os.MkdirAll(path, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0700); err != nil {
		return nil, err
	}
	return &Profile{path}, nil
}
func (p *Profile) Load(name string) (Object, error) {
	data, err := os.ReadFile(filepath.Join(p.Path, name+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var value Object
	if err = json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("cannot read saved %s: %w", name, err)
	}
	return value, nil
}
func (p *Profile) Save(name string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(p.Path, name+".json"), data)
}
func atomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".willys-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temp, path)
}
