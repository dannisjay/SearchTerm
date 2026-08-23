package magnetlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Record is one magnet that appeared in a search response.
type Record struct {
	Time     string `json:"time"`
	Keyword  string `json:"keyword"`
	Title    string `json:"title"`
	Site     string `json:"site"`
	InfoHash string `json:"info_hash"`
	Magnet   string `json:"magnet"`
	Size     string `json:"size"`
}

// Logger appends JSON lines and rotates by size, keeping maxFiles files.
type Logger struct {
	dir      string
	maxBytes int64
	maxFiles int

	mu   sync.Mutex
	file *os.File
	size int64
}

func New(dir string, maxBytes int64, maxFiles int) (*Logger, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	l := &Logger{dir: dir, maxBytes: maxBytes, maxFiles: maxFiles}
	if err := l.openCurrent(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

func (l *Logger) Append(records []Record) error {
	if len(records) == 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range records {
		line, err := json.Marshal(r)
		if err != nil {
			return err
		}
		line = append(line, '\n')
		if l.file == nil || l.size+int64(len(line)) > l.maxBytes {
			if err := l.rotate(); err != nil {
				return err
			}
		}
		n, err := l.file.Write(line)
		if err != nil {
			return err
		}
		l.size += int64(n)
	}
	return nil
}

func (l *Logger) openCurrent() error {
	names, err := l.files()
	if err != nil {
		return err
	}
	if len(names) > 0 {
		name := filepath.Join(l.dir, names[len(names)-1])
		fi, err := os.Stat(name)
		if err != nil {
			return err
		}
		f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		l.file = f
		l.size = fi.Size()
		return nil
	}
	return l.createNext(0)
}

func (l *Logger) rotate() error {
	if l.file != nil {
		if err := l.file.Close(); err != nil {
			l.file = nil
			return err
		}
		l.file = nil
	}
	names, err := l.files()
	if err != nil {
		return err
	}
	if len(names) >= l.maxFiles {
		removeN := l.maxFiles / 2
		if removeN < 1 {
			removeN = 1
		}
		for _, name := range names[:removeN] {
			if err := os.Remove(filepath.Join(l.dir, name)); err != nil {
				return err
			}
		}
	}
	names, err = l.files()
	if err != nil {
		return err
	}
	last := 0
	if len(names) > 0 {
		last = parseSeq(names[len(names)-1])
	}
	return l.createNext(last)
}

func (l *Logger) createNext(last int) error {
	name := filepath.Join(l.dir, fmt.Sprintf("magnet_%06d.jsonl", last+1))
	f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	l.file = f
	l.size = 0
	return nil
}

func (l *Logger) files() ([]string, error) {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "magnet_") && strings.HasSuffix(e.Name(), ".jsonl") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func parseSeq(name string) int {
	base := strings.TrimSuffix(strings.TrimPrefix(name, "magnet_"), ".jsonl")
	n, _ := strconv.Atoi(base)
	return n
}
