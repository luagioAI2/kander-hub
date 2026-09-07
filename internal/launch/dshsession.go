package launch

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dualface/kander/internal/fs"
	"github.com/klauspost/compress/zstd"
)

// dshSessionsRoot returns the DSH session store directory. It honours DSH_HOME
// the same way the DSH persistence layer does.
func dshSessionsRoot() string {
	home := strings.TrimSpace(os.Getenv("DSH_HOME"))
	if home == "" {
		userHome, _ := os.UserHomeDir()
		home = filepath.Join(userHome, ".dsh")
	}
	return filepath.Join(home, "sessions")
}

// dshProjectKey mirrors the projectKey of dsh-session-persistence-jsonl:
// path separators collapse to '-', other characters outside [A-Za-z0-9._-]
// become ~XXXX, and the whole key is wrapped in leading and trailing "--".
func dshProjectKey(cwd string) string {
	var readable []byte
	separatorRun := false
	for i := 0; i < len(cwd); i++ {
		ch := cwd[i]
		if ch == '/' || ch == '\\' || ch == ':' {
			if !separatorRun {
				readable = append(readable, '-')
			}
			separatorRun = true
			continue
		}
		separatorRun = false
		if ch == '~' || !isDshKeyByte(ch) {
			readable = append(readable, '~')
			readable = append(readable, []byte(hexUpper(ch, 16))...)
			continue
		}
		readable = append(readable, ch)
	}
	trimmed := bytes.TrimLeft(readable, "-")
	if len(trimmed) == 0 {
		trimmed = []byte("root")
	}
	if len(trimmed) > 251 {
		trimmed = trimmed[:251]
	}
	return "--" + string(trimmed) + "--"
}

func isDshKeyByte(ch byte) bool {
	return (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '.' || ch == '_' || ch == '-'
}

func hexUpper(value byte, width int) string {
	const digits = "0123456789ABCDEF"
	out := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		out[i] = digits[value&0x0f]
		value >>= 4
	}
	return string(out)
}

// dshEncodeSegment mirrors encodeSegment of dsh-session-persistence-jsonl.
func dshEncodeSegment(raw string) string {
	out := make([]byte, 0, len(raw)*2)
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if ch == '~' || !isDshKeyByte(ch) {
			out = append(out, '~')
			out = append(out, []byte(hexUpper(ch, 16))...)
			continue
		}
		out = append(out, ch)
	}
	return string(out)
}

// dshCompressFrame produces a single zstd frame for the payload.
func dshCompressFrame(payload []byte) ([]byte, error) {
	var buf bytes.Buffer
	encoder, err := zstd.NewWriter(&buf)
	if err != nil {
		return nil, err
	}
	if _, err := encoder.Write(payload); err != nil {
		_ = encoder.Close()
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// dshPrecreateSession writes an empty DSH session store entry with the stable
// per-task id and returns that id. Existing sessions are never overwritten.
func dshPrecreateSession(taskID, root string) (string, error) {
	sessionID := taskID
	dir := filepath.Join(dshSessionsRoot(), dshProjectKey(root), dshEncodeSegment(sessionID))
	logPath := filepath.Join(dir, "session.jsonl.zstd")
	if _, err := os.Stat(logPath); err == nil {
		return sessionID, nil
	}
	cwd, err := filepath.Abs(root)
	if err != nil {
		cwd = root
	}
	header := map[string]any{
		"type":            "session",
		"version":         0,
		"id":              sessionID,
		"createdAt":       time.Now().UnixMilli(),
		"delegationDepth": 0,
		"cwd":             cwd,
	}
	payload, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	payload = append(payload, '\n')
	compressed, err := dshCompressFrame(payload)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := fs.WriteTextAtomic(dir, "session.jsonl.zstd", string(compressed), false); err != nil {
		return "", err
	}
	return sessionID, nil
}

// taskLaunchRoot returns the working directory DSH sessions are grouped under.
// DSH keys sessions by the cwd of the launched process, which is the kanban
// directory (the parent of the board root), matching the launch plan cwd.
func taskLaunchRoot() string {
	root, err := boardRootFn()
	if err == nil && root != "" {
		return parentDir(root)
	}
	cwd, _ := os.Getwd()
	return cwd
}

// findDshSession locates the newest DSH session recorded for the task. The
// caller supplies the project root because DSH groups sessions by cwd.
func findDshSession(taskID, root string) (string, error) {
	root = filepath.Join(dshSessionsRoot(), dshProjectKey(root))
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", launchError(
			"launch.dsh_sessions_directory_not_found_cannot_resume_with_context", root,
		)
	}
	var matches []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		logPath := filepath.Join(root, entry.Name(), "session.jsonl.zstd")
		id, err := readDshSessionID(logPath)
		if err != nil || id == "" {
			continue
		}
		if dshSessionMatchesTask(id, taskID) {
			matches = append(matches, id)
		}
	}
	if len(matches) == 0 {
		return "", launchError(
			"launch.no_dsh_execution_session_started_for_was_found_under", root, taskID,
		)
	}
	return matches[0], nil
}

// readDshSessionID decompresses a session.jsonl.zstd file and returns the id
// recorded in the leading JSON header object.
func readDshSessionID(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	decoder, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	defer decoder.Close()
	decompressed, err := decoder.DecodeAll(data, nil)
	if err != nil {
		return "", err
	}
	head := decompressed
	if idx := bytes.IndexByte(decompressed, '\n'); idx >= 0 {
		head = decompressed[:idx]
	}
	var header struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(head, &header); err != nil {
		return "", err
	}
	return header.ID, nil
}

// dshSessionMatchesTask reports whether a stable session id belongs to the
// task. Kander-hub ids use the "<task-id>" form directly.
func dshSessionMatchesTask(sessionID, taskID string) bool {
	return sessionID == taskID || len(sessionID) > len(taskID) && sessionID[:len(taskID)+1] == taskID+"-"
}
