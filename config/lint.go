package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// LintDiagnostic describes one structural problem in a config file.
type LintDiagnostic struct {
	Path    string `json:"path"`
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
	Message string `json:"message"`
}

func (d LintDiagnostic) String() string {
	loc := d.Path
	if d.Line > 0 {
		loc = fmt.Sprintf("%s:%d:%d", loc, d.Line, d.Column)
	}
	return fmt.Sprintf("%s: %s", loc, d.Message)
}

type lintOptions struct {
	knownProviders map[string]struct{}
}

// LintOption customizes config linting.
type LintOption func(*lintOptions)

// WithKnownProviders makes lint reject provider names unseat cannot build.
func WithKnownProviders(names []string) LintOption {
	return func(o *lintOptions) {
		o.knownProviders = make(map[string]struct{}, len(names))
		for _, name := range names {
			o.knownProviders[name] = struct{}{}
		}
	}
}

// Lint validates the YAML structure, accepted keys and concrete scalar formats.
//
// It deliberately does not expand environment variables. A config using
// ${LINEAR_API_KEY} can be linted in CI without exposing the secret; Load still
// expands and rejects missing variables when a command actually runs.
func Lint(path string, opts ...LintOption) ([]LintDiagnostic, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return LintBytes(path, data, opts...), nil
}

// LintBytes is the testable form of Lint.
func LintBytes(path string, data []byte, opts ...LintOption) []LintDiagnostic {
	options := lintOptions{}
	for _, opt := range opts {
		opt(&options)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return []LintDiagnostic{{
			Path:    path,
			Message: "parse YAML: " + err.Error(),
		}}
	}

	root := documentRoot(&doc)
	if root == nil || isNull(root) {
		return nil
	}

	l := configLinter{options: options}
	l.lintRoot(root)
	return l.diagnostics
}

type configLinter struct {
	options     lintOptions
	diagnostics []LintDiagnostic
}

func documentRoot(doc *yaml.Node) *yaml.Node {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil
	}
	return doc.Content[0]
}

func (l *configLinter) lintRoot(n *yaml.Node) {
	if !l.expectMap("$", n, false) {
		return
	}
	l.checkDuplicates("$", n)

	forEachPair(n, func(k, v *yaml.Node) {
		path := k.Value
		switch k.Value {
		case "identity_source":
			l.lintIdentitySource(path, v)
		case "domain":
			l.expectScalar(path, v)
		case "providers":
			l.lintProviders(path, v)
		case "mappings":
			l.lintMappings(path, v)
		case "aliases":
			l.lintAliases(path, v)
		case "policies":
			l.lintPolicies(path, v)
		case "currency":
			l.removed(k, path, "billing is API-only; remove currency from config")
		default:
			l.unknown(k, path)
		}
	})
}

func (l *configLinter) lintIdentitySource(path string, n *yaml.Node) {
	if !l.expectMap(path, n, true) {
		return
	}
	if isNull(n) {
		return
	}
	l.checkDuplicates(path, n)

	forEachPair(n, func(k, v *yaml.Node) {
		child := joinPath(path, k.Value)
		switch k.Value {
		case "provider":
			l.expectScalar(child, v)
			if isConcreteScalar(v) {
				l.expectKnownProvider(child, k, v.Value)
			}
		case "domain", "credentials_file", "admin_email":
			l.expectScalar(child, v)
		case "allow_write":
			l.expectBool(child, v)
		default:
			l.unknown(k, child)
		}
	})
}

func (l *configLinter) lintProviders(path string, n *yaml.Node) {
	if !l.expectMap(path, n, true) {
		return
	}
	if isNull(n) {
		return
	}
	l.checkDuplicates(path, n)

	forEachPair(n, func(k, v *yaml.Node) {
		providerPath := joinPath(path, k.Value)
		l.expectKnownProvider(providerPath, k, k.Value)
		l.lintProviderConfig(providerPath, v)
	})
}

func (l *configLinter) lintProviderConfig(path string, n *yaml.Node) {
	if isNull(n) {
		return
	}
	if !l.expectMap(path, n, false) {
		return
	}
	l.checkDuplicates(path, n)

	forEachPair(n, func(k, v *yaml.Node) {
		child := joinPath(path, k.Value)
		switch k.Value {
		case "api_key", "base_url":
			l.expectScalar(child, v)
		case "extra":
			l.lintStringMap(child, v)
		case "cost_per_seat", "monthly_spend", "bills_suspended_seats":
			l.removed(k, child, "billing is API-only; remove manual pricing fields from provider config")
		default:
			l.unknown(k, child)
		}
	})
}

func (l *configLinter) lintStringMap(path string, n *yaml.Node) {
	if !l.expectMap(path, n, true) {
		return
	}
	if isNull(n) {
		return
	}
	l.checkDuplicates(path, n)

	forEachPair(n, func(k, v *yaml.Node) {
		l.expectScalar(joinPath(path, k.Value), v)
	})
}

func (l *configLinter) lintMappings(path string, n *yaml.Node) {
	if !l.expectSeq(path, n, true) {
		return
	}
	if isNull(n) {
		return
	}
	for i, item := range n.Content {
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		if !l.expectMap(itemPath, item, false) {
			continue
		}
		l.checkDuplicates(itemPath, item)
		forEachPair(item, func(k, v *yaml.Node) {
			child := joinPath(itemPath, k.Value)
			switch k.Value {
			case "group":
				l.expectScalar(child, v)
			case "providers":
				l.lintMappingProviders(child, v)
			default:
				l.unknown(k, child)
			}
		})
	}
}

func (l *configLinter) lintMappingProviders(path string, n *yaml.Node) {
	if !l.expectSeq(path, n, true) {
		return
	}
	if isNull(n) {
		return
	}
	for i, item := range n.Content {
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		if !l.expectMap(itemPath, item, false) {
			continue
		}
		l.checkDuplicates(itemPath, item)
		forEachPair(item, func(k, v *yaml.Node) {
			child := joinPath(itemPath, k.Value)
			switch k.Value {
			case "name":
				l.expectScalar(child, v)
				if isConcreteScalar(v) {
					l.expectKnownProvider(child, k, v.Value)
				}
			case "role":
				l.expectScalar(child, v)
			default:
				l.unknown(k, child)
			}
		})
	}
}

func (l *configLinter) lintAliases(path string, n *yaml.Node) {
	if !l.expectMap(path, n, true) {
		return
	}
	if isNull(n) {
		return
	}
	l.checkDuplicates(path, n)

	forEachPair(n, func(k, v *yaml.Node) {
		child := joinPath(path, k.Value)
		if !l.expectSeq(child, v, false) {
			return
		}
		for i, item := range v.Content {
			l.expectScalar(fmt.Sprintf("%s[%d]", child, i), item)
		}
	})
}

func (l *configLinter) lintPolicies(path string, n *yaml.Node) {
	if !l.expectMap(path, n, true) {
		return
	}
	if isNull(n) {
		return
	}
	l.checkDuplicates(path, n)

	forEachPair(n, func(k, v *yaml.Node) {
		child := joinPath(path, k.Value)
		switch k.Value {
		case "grace_period", "sync_interval":
			l.expectDuration(child, v)
		case "dry_run", "notify_on_remove":
			l.expectBool(child, v)
		case "notify_channels":
			l.lintStringSeq(child, v)
		case "exceptions":
			l.lintExceptions(child, v)
		case "notify":
			l.lintNotify(child, v)
		default:
			l.unknown(k, child)
		}
	})
}

func (l *configLinter) lintStringSeq(path string, n *yaml.Node) {
	if !l.expectSeq(path, n, true) {
		return
	}
	if isNull(n) {
		return
	}
	for i, item := range n.Content {
		l.expectScalar(fmt.Sprintf("%s[%d]", path, i), item)
	}
}

func (l *configLinter) lintExceptions(path string, n *yaml.Node) {
	if !l.expectSeq(path, n, true) {
		return
	}
	if isNull(n) {
		return
	}
	for i, item := range n.Content {
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		if !l.expectMap(itemPath, item, false) {
			continue
		}
		l.checkDuplicates(itemPath, item)
		forEachPair(item, func(k, v *yaml.Node) {
			child := joinPath(itemPath, k.Value)
			switch k.Value {
			case "email":
				l.expectScalar(child, v)
			case "providers":
				l.expectProviderSeq(child, v)
			default:
				l.unknown(k, child)
			}
		})
	}
}

func (l *configLinter) expectProviderSeq(path string, n *yaml.Node) {
	if !l.expectSeq(path, n, false) {
		return
	}
	for i, item := range n.Content {
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		l.expectScalar(itemPath, item)
		if isConcreteScalar(item) && item.Value != "*" {
			l.expectKnownProvider(itemPath, item, item.Value)
		}
	}
}

func (l *configLinter) lintNotify(path string, n *yaml.Node) {
	if !l.expectMap(path, n, true) {
		return
	}
	if isNull(n) {
		return
	}
	l.checkDuplicates(path, n)

	forEachPair(n, func(k, v *yaml.Node) {
		child := joinPath(path, k.Value)
		switch k.Value {
		case "slack_webhook_url", "smtp_host", "smtp_from", "smtp_user", "smtp_pass":
			l.expectScalar(child, v)
		case "smtp_port":
			l.expectInt(child, v)
		default:
			l.unknown(k, child)
		}
	})
}

func (l *configLinter) expectMap(path string, n *yaml.Node, nullable bool) bool {
	if nullable && isNull(n) {
		return true
	}
	if n.Kind != yaml.MappingNode {
		l.add(n, path, "must be a mapping")
		return false
	}
	return true
}

func (l *configLinter) expectSeq(path string, n *yaml.Node, nullable bool) bool {
	if nullable && isNull(n) {
		return true
	}
	if n.Kind != yaml.SequenceNode {
		l.add(n, path, "must be a sequence")
		return false
	}
	return true
}

func (l *configLinter) expectScalar(path string, n *yaml.Node) bool {
	if n.Kind != yaml.ScalarNode || isNull(n) {
		l.add(n, path, "must be a scalar value")
		return false
	}
	return true
}

func (l *configLinter) expectBool(path string, n *yaml.Node) {
	if !l.expectScalar(path, n) || hasEnvReference(n.Value) {
		return
	}
	if _, err := strconv.ParseBool(n.Value); err != nil {
		l.add(n, path, "must be a boolean")
	}
}

func (l *configLinter) expectInt(path string, n *yaml.Node) {
	if !l.expectScalar(path, n) || hasEnvReference(n.Value) {
		return
	}
	if _, err := strconv.Atoi(n.Value); err != nil {
		l.add(n, path, "must be an integer")
	}
}

func (l *configLinter) expectDuration(path string, n *yaml.Node) {
	if !l.expectScalar(path, n) || hasEnvReference(n.Value) {
		return
	}
	if _, err := time.ParseDuration(n.Value); err != nil {
		l.add(n, path, "must be a duration such as 72h or 10m")
	}
}

func (l *configLinter) expectKnownProvider(path string, n *yaml.Node, name string) {
	if len(l.options.knownProviders) == 0 || hasEnvReference(name) {
		return
	}
	if _, ok := l.options.knownProviders[name]; !ok {
		l.add(n, path, fmt.Sprintf("unknown provider %q", name))
	}
}

func (l *configLinter) checkDuplicates(path string, n *yaml.Node) {
	seen := make(map[string]*yaml.Node)
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i]
		if key.Kind != yaml.ScalarNode {
			l.add(key, path, "mapping keys must be scalar strings")
			continue
		}
		if previous := seen[key.Value]; previous != nil {
			l.add(key, joinPath(path, key.Value), fmt.Sprintf("duplicate key, first defined at line %d", previous.Line))
			continue
		}
		seen[key.Value] = key
	}
}

func (l *configLinter) unknown(n *yaml.Node, path string) {
	l.add(n, path, "unknown config key")
}

func (l *configLinter) removed(n *yaml.Node, path string, message string) {
	l.add(n, path, "removed config key: "+message)
}

func (l *configLinter) add(n *yaml.Node, path string, message string) {
	l.diagnostics = append(l.diagnostics, LintDiagnostic{
		Path:    path,
		Line:    n.Line,
		Column:  n.Column,
		Message: message,
	})
}

func forEachPair(n *yaml.Node, fn func(key, value *yaml.Node)) {
	for i := 0; i+1 < len(n.Content); i += 2 {
		fn(n.Content[i], n.Content[i+1])
	}
}

func isNull(n *yaml.Node) bool {
	return n == nil || (n.Kind == yaml.ScalarNode && n.Tag == "!!null")
}

func isConcreteScalar(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && !isNull(n) && !hasEnvReference(n.Value)
}

func hasEnvReference(value string) bool {
	return strings.Contains(value, "${")
}

func joinPath(parent, child string) string {
	if parent == "" || parent == "$" {
		return child
	}
	return parent + "." + child
}
