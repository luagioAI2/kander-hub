package usage

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dualface/kander/internal/launch"
	"github.com/klauspost/compress/zstd"
)

func dshRoot() string { return launch.DshSessionsRoot() }

// scanDsh reads DSH session stores. A store is one zstd-compressed JSON Lines file
// per session, grouped by a directory derived from the launch working directory.
func scanDsh(root string, opts Options) ([]Session, string, error) {
	if root == "" {
		return nil, "", nil
	}
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, "session directory does not exist", nil
		}
		return nil, "", err
	}

	var sessions []Session
	projects, err := os.ReadDir(root)
	if err != nil {
		return nil, "", err
	}
	for _, project := range projects {
		if !project.IsDir() {
			continue
		}
		found, err := scanDshProject(filepath.Join(root, project.Name()), root)
		if err != nil {
			return nil, "", err
		}
		sessions = append(sessions, found...)
	}
	return sessions, "", nil
}

func scanDshProject(dir, root string) ([]Session, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var sessions []Session
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name(), "session.jsonl.zstd")
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			continue
		}
		session, err := parseDshSession(path, entry.Name(), root)
		if err != nil {
			// A session mid-write or from a different schema must not fail the whole
			// report; it is simply not counted.
			continue
		}
		sessions = append(sessions, session)
	}
	return sessions, nil
}

// parseDshSession extracts usage from one DSH session log.
//
// Counting rule: DSH writes the same usage object twice per API call, once as a
// streaming chunk (assistant/chunk -> chunk.usage) and once on the finalized
// message (assistant/message -> usage). Their values are identical, so summing both
// would report exactly double. Only the chunk form is counted here because it is
// the superset: it also carries usage for calls whose message was never finalized,
// such as aborted attempts.
func parseDshSession(path, fallbackID, projectDir string) (Session, error) {
	file, err := os.Open(path)
	if err != nil {
		return Session{}, err
	}
	defer file.Close()

	decoder, err := zstd.NewReader(file)
	if err != nil {
		return Session{}, err
	}
	defer decoder.Close()

	session := Session{
		Agent: "dsh",
		ID:    fallbackID,
		Path:  path,
		Cwd:   dshProjectCwd(projectDir),
	}
	reader := bufio.NewScanner(decoder)
	reader.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for reader.Scan() {
		line := reader.Bytes()
		if len(line) == 0 {
			continue
		}
		var record dshRecord
		if err := json.Unmarshal(line, &record); err != nil {
			continue
		}
		switch record.Type {
		case "session":
			if record.ID != "" {
				session.ID = record.ID
			}
			if record.Cwd != "" {
				session.Cwd = record.Cwd
			}
			if record.CreatedAt > 0 {
				session.First = millis(record.CreatedAt)
			}
		case "request/header":
			if record.Data.Header.Config.Model != "" {
				session.Model = record.Data.Header.Config.Model
			}
		case "assistant/chunk":
			usage := record.Data.Chunk.Usage
			if usage == nil {
				continue
			}
			session.Counts = session.Counts.Add(Counts{
				CacheRead: usage.CacheReadTokens,
				Input:     usage.InputTokens,
				Output:    usage.OutputTokens,
			})
			session.Steps++
			if at := millis(record.Time); !at.IsZero() {
				if session.First.IsZero() || at.Before(session.First) {
					session.First = at
				}
				if at.After(session.Last) {
					session.Last = at
				}
			}
		}
	}
	return session, nil
}

// dshProjectCwd recovers the working directory from the encoded project key, which
// is only a fallback: the session header carries the exact cwd for every session
// written by a current DSH build.
func dshProjectCwd(projectKey string) string {
	trimmed := strings.Trim(projectKey, "-")
	var out strings.Builder
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] == '~' && i+2 < len(trimmed) {
			out.WriteByte(unescapeDshByte(trimmed[i+1], trimmed[i+2]))
			i += 2
			continue
		}
		if trimmed[i] == '-' {
			out.WriteByte(filepath.Separator)
			continue
		}
		out.WriteByte(trimmed[i])
	}
	return out.String()
}

func unescapeDshByte(high, low byte) byte {
	return hexValue(high)<<4 | hexValue(low)
}

func hexValue(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	}
	return 0
}

func millis(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(value)
}

// dshRecord is the envelope every DSH session line shares. Absent sections stay
// zero-valued, so one struct covers the session header, the request header and the
// assistant chunk without a second parse.
type dshRecord struct {
	Type      string `json:"type"`
	Time      int64  `json:"time"`
	ID        string `json:"id"`
	Cwd       string `json:"cwd"`
	CreatedAt int64  `json:"createdAt"`
	Data      struct {
		Chunk struct {
			Usage *dshUsage `json:"usage"`
		} `json:"chunk"`
		Header struct {
			Config struct {
				Model    string `json:"model"`
				Provider string `json:"provider"`
			} `json:"config"`
		} `json:"header"`
	} `json:"data"`
}

type dshUsage struct {
	CacheReadTokens int64 `json:"cacheReadTokens"`
	InputTokens     int64 `json:"inputTokens"`
	OutputTokens    int64 `json:"outputTokens"`
	TotalTokens     int64 `json:"totalTokens"`
}
