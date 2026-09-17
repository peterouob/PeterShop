package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Section string

const (
	SectionMySQL Section = "mysql"
	SectionRedis Section = "redis"
	SectionKafka Section = "kafka"
	SectionEtcd  Section = "etcd"
	SectionJWT   Section = "jwt"
)

type loader struct {
	problems []string
	sections map[Section]struct{}
}

func newLoader(sections []Section) *loader {
	set := make(map[Section]struct{}, len(sections))
	for _, s := range sections {
		set[s] = struct{}{}
	}
	return &loader{sections: set}
}

func (l *loader) needs(s Section) bool {
	_, ok := l.sections[s]
	return ok
}

func (l *loader) secret(s Section, key string) string {
	if l.needs(s) {
		return l.required(key)
	}
	return l.str(key, "")
}

func (l *loader) fail(key, reason string) {
	l.problems = append(l.problems, fmt.Sprintf("%s: %s", key, reason))
}

func (l *loader) lookup(key string) (string, bool) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return "", false
	}
	if v = strings.TrimSpace(v); v == "" {
		return "", false
	}
	return v, true
}

func (l *loader) str(key, fallback string) string {
	if v, ok := l.lookup(key); ok {
		return v
	}
	return fallback
}

func (l *loader) required(key string) string {
	v, ok := l.lookup(key)
	if !ok {
		l.fail(key, "is required but not set")
	}
	return v
}

func (l *loader) int(key string, fallback int) int {
	v, ok := l.lookup(key)
	if !ok {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		l.fail(key, fmt.Sprintf("expects an integer, got %q", v))
		return fallback
	}
	return n
}

func (l *loader) duration(key string, fallback time.Duration) time.Duration {
	v, ok := l.lookup(key)
	if !ok {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		l.fail(key, fmt.Sprintf("expects a duration such as %q, got %q", fallback.String(), v))
		return fallback
	}
	return d
}

func (l *loader) list(key string, fallback []string) []string {
	v, ok := l.lookup(key)
	if !ok {
		return fallback
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		l.fail(key, "must contain at least one non-empty entry")
	}
	return out
}

func (l *loader) err() error {
	if len(l.problems) == 0 {
		return nil
	}
	return fmt.Errorf("invalid configuration:\n  - %s", strings.Join(l.problems, "\n  - "))
}

var ErrMissingServiceName = errors.New("config: service name must not be empty")
