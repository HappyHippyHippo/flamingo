package flam

import (
	"os"
	"strconv"
	"strings"
)

// EnvBool @todo doc
func EnvBool(name string, fallback ...bool) bool {
	f := false
	if len(fallback) > 0 {
		f = fallback[0]
	}
	value, ok := os.LookupEnv(name)
	if !ok || value == "" {
		return f
	}
	parsed, e := strconv.ParseBool(value)
	if e != nil {
		return f
	}
	return parsed
}

// EnvInt @todo doc
func EnvInt(name string, fallback ...int) int {
	f := 0
	if len(fallback) > 0 {
		f = fallback[0]
	}
	value, ok := os.LookupEnv(name)
	if !ok || value == "" {
		return f
	}
	parsed, e := strconv.Atoi(value)
	if e != nil {
		return f
	}
	return parsed
}

// EnvString @todo doc
func EnvString(name string, fallback ...string) string {
	f := ""
	if len(fallback) > 0 {
		f = fallback[0]
	}
	value, ok := os.LookupEnv(name)
	if !ok || value == "" {
		return f
	}
	return value
}

// EnvList @todo doc
func EnvList(name string, fallback ...[]string) []string {
	var f []string
	if len(fallback) > 0 {
		f = fallback[0]
	}
	value, ok := os.LookupEnv(name)
	if !ok || value == "" {
		return f
	}
	var r []string
	for _, v := range strings.Split(value, ",") {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		r = append(r, v)
	}
	return r
}

// EnvLogLevel @todo doc
func EnvLogLevel(name string, fallback ...LogLevel) LogLevel {
	f := LogInfo
	if len(fallback) > 0 {
		f = fallback[0]
	}
	return LogLevelMap[EnvString(name, LogLevelName[f])]
}
