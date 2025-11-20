package blacklist

import (
	"bufio"
	"os"
	"strings"
)

type List struct {
	words []string
}

func Load(path string) (*List, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var w []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		t := strings.TrimSpace(sc.Text())
		if t != "" && !strings.HasPrefix(t, "//") {
			w = append(w, strings.ToLower(t))
		}
	}
	return &List{words: w}, sc.Err()
}

func (l *List) Contains(s string) bool {
	msg := strings.ToLower(s)
	for _, w := range l.words {
		if strings.Contains(msg, w) {
			return true
		}
	}
	return false
}

func (l *List) Words() []string {
	return append([]string(nil), l.words...)
}
