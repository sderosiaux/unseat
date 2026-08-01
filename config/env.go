package config

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// envVarPattern matches ${VAR} references in the raw YAML.
var envVarPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// ExpandEnv substitutes ${VAR} references with their environment values.
//
// Undefined variables are an error rather than a silent pass-through: a
// literal "${LINEAR_API_KEY}" would otherwise be sent as a bearer token and
// fail as an opaque 401 from the provider.
func ExpandEnv(data []byte) ([]byte, error) {
	missing := make(map[string]bool)

	out := envVarPattern.ReplaceAllFunc(data, func(match []byte) []byte {
		name := string(envVarPattern.FindSubmatch(match)[1])
		val, ok := os.LookupEnv(name)
		if !ok {
			missing[name] = true
			return match
		}
		return []byte(val)
	})

	if len(missing) > 0 {
		names := make([]string, 0, len(missing))
		for n := range missing {
			names = append(names, n)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("undefined environment variables: %s (define them or set the value inline)", strings.Join(names, ", "))
	}

	return out, nil
}

// LoadDotEnv reads KEY=VALUE pairs from path into the process environment.
// Existing environment variables always win, so an explicit `export` in the
// shell overrides the file. A missing file is not an error.
func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")

		key, value, found := strings.Cut(line, "=")
		if !found {
			return fmt.Errorf("%s:%d: expected KEY=VALUE", path, lineNo)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		// Strip matching surrounding quotes; leave inner content untouched.
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("%s:%d: set %s: %w", path, lineNo, key, err)
		}
	}
	return scanner.Err()
}
