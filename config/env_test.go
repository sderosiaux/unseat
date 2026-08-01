package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scopedUnset guarantees the named variables are undefined for the duration of
// the test, and restores whatever the process had before — including for
// variables the code under test sets itself via os.Setenv.
func scopedUnset(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		t.Setenv(name, "") // registers the restore-on-cleanup hook
		require.NoError(t, os.Unsetenv(name))
	}
}

// writeEnvFile writes a .env file into a per-test temp dir and returns its path.
func writeEnvFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestExpandEnv(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		in   string
		want string
	}{
		{
			name: "substitutes a single ${VAR}",
			env:  map[string]string{"UNSEAT_TEST_KEY": "sk-123"},
			in:   `api_key: "${UNSEAT_TEST_KEY}"`,
			want: `api_key: "sk-123"`,
		},
		{
			name: "substitutes several distinct vars",
			env:  map[string]string{"UNSEAT_TEST_A": "aaa", "UNSEAT_TEST_B": "bbb"},
			in:   "a: ${UNSEAT_TEST_A}\nb: ${UNSEAT_TEST_B}\n",
			want: "a: aaa\nb: bbb\n",
		},
		{
			name: "substitutes the same var twice",
			env:  map[string]string{"UNSEAT_TEST_A": "aaa"},
			in:   "x: ${UNSEAT_TEST_A}\ny: ${UNSEAT_TEST_A}",
			want: "x: aaa\ny: aaa",
		},
		{
			name: "leaves bare $VAR untouched",
			env:  map[string]string{"UNSEAT_TEST_A": "aaa"},
			in:   "x: $UNSEAT_TEST_A",
			want: "x: $UNSEAT_TEST_A",
		},
		{
			name: "leaves a lone $ untouched",
			in:   "price: $ and $$",
			want: "price: $ and $$",
		},
		{
			name: "leaves ${} with no name untouched",
			in:   "x: ${}",
			want: "x: ${}",
		},
		{
			name: "substitutes an empty but defined var",
			env:  map[string]string{"UNSEAT_TEST_EMPTY": ""},
			in:   "x: '${UNSEAT_TEST_EMPTY}'",
			want: "x: ''",
		},
		{
			name: "no references is a passthrough",
			in:   "providers:\n  linear:\n    api_key: inline\n",
			want: "providers:\n  linear:\n    api_key: inline\n",
		},
		{
			name: "empty input",
			in:   "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			out, err := ExpandEnv([]byte(tt.in))
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(out))
		})
	}
}

func TestExpandEnv_UndefinedVariables(t *testing.T) {
	t.Run("names the undefined variable and returns nil data", func(t *testing.T) {
		scopedUnset(t, "UNSEAT_TEST_MISSING")

		out, err := ExpandEnv([]byte(`api_key: "${UNSEAT_TEST_MISSING}"`))
		require.Error(t, err)
		assert.Nil(t, out, "data must be nil so a caller cannot use a half-expanded config")
		assert.Contains(t, err.Error(), "UNSEAT_TEST_MISSING")
	})

	t.Run("names every undefined variable, sorted and deduplicated", func(t *testing.T) {
		scopedUnset(t, "UNSEAT_ZED", "UNSEAT_ALPHA", "UNSEAT_MID")
		t.Setenv("UNSEAT_TEST_DEFINED", "ok")

		_, err := ExpandEnv([]byte(
			"a: ${UNSEAT_ZED}\n" +
				"b: ${UNSEAT_MID}\n" +
				"c: ${UNSEAT_ALPHA}\n" +
				"d: ${UNSEAT_MID}\n" + // duplicate, must be reported once
				"e: ${UNSEAT_TEST_DEFINED}\n",
		))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "UNSEAT_ALPHA, UNSEAT_MID, UNSEAT_ZED")
		assert.NotContains(t, err.Error(), "UNSEAT_TEST_DEFINED",
			"a defined variable must not be listed as missing")
	})

	t.Run("one missing var among defined ones still fails", func(t *testing.T) {
		scopedUnset(t, "UNSEAT_TEST_MISSING")
		t.Setenv("UNSEAT_TEST_PRESENT", "value")

		out, err := ExpandEnv([]byte("a: ${UNSEAT_TEST_PRESENT}\nb: ${UNSEAT_TEST_MISSING}\n"))
		require.Error(t, err)
		assert.Nil(t, out)
	})
}

func TestLoadDotEnv(t *testing.T) {
	tests := []struct {
		name    string
		content string
		keys    []string
		want    map[string]string
	}{
		{
			name: "plain KEY=VALUE",
			content: "UNSEAT_DOTENV_A=alpha\n" +
				"UNSEAT_DOTENV_B=beta\n",
			keys: []string{"UNSEAT_DOTENV_A", "UNSEAT_DOTENV_B"},
			want: map[string]string{"UNSEAT_DOTENV_A": "alpha", "UNSEAT_DOTENV_B": "beta"},
		},
		{
			name: "comments and blank lines are skipped",
			content: "# a comment\n" +
				"\n" +
				"   \n" +
				"  # indented comment\n" +
				"UNSEAT_DOTENV_A=alpha\n",
			keys: []string{"UNSEAT_DOTENV_A"},
			want: map[string]string{"UNSEAT_DOTENV_A": "alpha"},
		},
		{
			name:    "export prefix is stripped",
			content: "export UNSEAT_DOTENV_A=alpha\n  export UNSEAT_DOTENV_B=beta\n",
			keys:    []string{"UNSEAT_DOTENV_A", "UNSEAT_DOTENV_B"},
			want:    map[string]string{"UNSEAT_DOTENV_A": "alpha", "UNSEAT_DOTENV_B": "beta"},
		},
		{
			name:    "double quotes are stripped",
			content: `UNSEAT_DOTENV_A="alpha beta"` + "\n",
			keys:    []string{"UNSEAT_DOTENV_A"},
			want:    map[string]string{"UNSEAT_DOTENV_A": "alpha beta"},
		},
		{
			name:    "single quotes are stripped",
			content: "UNSEAT_DOTENV_A='alpha beta'\n",
			keys:    []string{"UNSEAT_DOTENV_A"},
			want:    map[string]string{"UNSEAT_DOTENV_A": "alpha beta"},
		},
		{
			name:    "inner quotes survive outer stripping",
			content: `UNSEAT_DOTENV_A="say ""hi"" now"` + "\n",
			keys:    []string{"UNSEAT_DOTENV_A"},
			want:    map[string]string{"UNSEAT_DOTENV_A": `say ""hi"" now`},
		},
		{
			name:    "mismatched quotes are left alone",
			content: "UNSEAT_DOTENV_A='alpha\"\n",
			keys:    []string{"UNSEAT_DOTENV_A"},
			want:    map[string]string{"UNSEAT_DOTENV_A": `'alpha"`},
		},
		{
			name:    "only the first = splits the line",
			content: "UNSEAT_DOTENV_A=postgres://u:p@h/db?sslmode=require&x=1\n",
			keys:    []string{"UNSEAT_DOTENV_A"},
			want:    map[string]string{"UNSEAT_DOTENV_A": "postgres://u:p@h/db?sslmode=require&x=1"},
		},
		{
			name:    "base64 padding is preserved",
			content: "UNSEAT_DOTENV_A=YWxwaGE=\n",
			keys:    []string{"UNSEAT_DOTENV_A"},
			want:    map[string]string{"UNSEAT_DOTENV_A": "YWxwaGE="},
		},
		{
			name:    "empty value",
			content: "UNSEAT_DOTENV_A=\n",
			keys:    []string{"UNSEAT_DOTENV_A"},
			want:    map[string]string{"UNSEAT_DOTENV_A": ""},
		},
		{
			name:    "whitespace around key and value is trimmed",
			content: "  UNSEAT_DOTENV_A  =   alpha   \n",
			keys:    []string{"UNSEAT_DOTENV_A"},
			want:    map[string]string{"UNSEAT_DOTENV_A": "alpha"},
		},
		{
			name:    "last line without a trailing newline is read",
			content: "UNSEAT_DOTENV_A=alpha",
			keys:    []string{"UNSEAT_DOTENV_A"},
			want:    map[string]string{"UNSEAT_DOTENV_A": "alpha"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scopedUnset(t, tt.keys...)

			require.NoError(t, LoadDotEnv(writeEnvFile(t, tt.content)))

			for k, want := range tt.want {
				got, ok := os.LookupEnv(k)
				require.True(t, ok, "%s should have been set", k)
				assert.Equal(t, want, got)
			}
		})
	}
}

func TestLoadDotEnv_DoesNotOverwriteExistingEnv(t *testing.T) {
	t.Setenv("UNSEAT_DOTENV_A", "from-shell")
	scopedUnset(t, "UNSEAT_DOTENV_B")

	require.NoError(t, LoadDotEnv(writeEnvFile(t,
		"UNSEAT_DOTENV_A=from-file\nUNSEAT_DOTENV_B=from-file\n",
	)))

	assert.Equal(t, "from-shell", os.Getenv("UNSEAT_DOTENV_A"),
		"an explicit export in the shell must win over the file")
	assert.Equal(t, "from-file", os.Getenv("UNSEAT_DOTENV_B"))
}

func TestLoadDotEnv_ExistingEmptyValueStillWins(t *testing.T) {
	t.Setenv("UNSEAT_DOTENV_A", "") // defined, but empty

	require.NoError(t, LoadDotEnv(writeEnvFile(t, "UNSEAT_DOTENV_A=from-file\n")))

	assert.Equal(t, "", os.Getenv("UNSEAT_DOTENV_A"),
		"defined-but-empty is still defined; the file must not fill it in")
}

func TestLoadDotEnv_MalformedLine(t *testing.T) {
	scopedUnset(t, "UNSEAT_DOTENV_A", "UNSEAT_DOTENV_B")

	path := writeEnvFile(t,
		"# header\n"+
			"UNSEAT_DOTENV_A=alpha\n"+
			"THIS_LINE_HAS_NO_EQUALS\n"+
			"UNSEAT_DOTENV_B=beta\n",
	)

	err := LoadDotEnv(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), fmt.Sprintf("%s:3", path), "the error must point at the offending line")
	assert.Contains(t, err.Error(), "expected KEY=VALUE")

	assert.Equal(t, "alpha", os.Getenv("UNSEAT_DOTENV_A"), "lines before the failure are applied")
	_, ok := os.LookupEnv("UNSEAT_DOTENV_B")
	assert.False(t, ok, "parsing stops at the first malformed line")
}

func TestLoadDotEnv_MissingFileIsNoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.env")
	assert.NoError(t, LoadDotEnv(path))
}

func TestLoadDotEnv_EmptyFile(t *testing.T) {
	assert.NoError(t, LoadDotEnv(writeEnvFile(t, "")))
}

func TestLoadDotEnv_FeedsExpandEnv(t *testing.T) {
	scopedUnset(t, "UNSEAT_DOTENV_KEY")

	require.NoError(t, LoadDotEnv(writeEnvFile(t, `UNSEAT_DOTENV_KEY="sk-live-42"`+"\n")))

	out, err := ExpandEnv([]byte(`api_key: "${UNSEAT_DOTENV_KEY}"`))
	require.NoError(t, err)
	assert.Equal(t, `api_key: "sk-live-42"`, string(out))
}
