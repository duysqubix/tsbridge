package doccheck_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/jtdowney/tsbridge/internal/config"
)

var markdownLink = regexp.MustCompile(`!?\[[^]]*\]\(([^)]+)\)`)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func markdownAnchors(text string) map[string]bool {
	anchors := map[string]bool{}
	counts := map[string]int{}
	for _, line := range strings.Split(text, "\n") {
		heading := strings.TrimSpace(line)
		if !strings.HasPrefix(heading, "#") {
			continue
		}
		heading = strings.TrimSpace(strings.TrimLeft(heading, "#"))
		if heading == "" {
			continue
		}
		slug := strings.Map(func(r rune) rune {
			switch {
			case r >= 'A' && r <= 'Z':
				return r + ('a' - 'A')
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == ' ':
				return r
			default:
				return -1
			}
		}, heading)
		slug = strings.Join(strings.Fields(slug), "-")
		if count := counts[slug]; count > 0 {
			anchors[slug+"-"+strconv.Itoa(count)] = true
		} else {
			anchors[slug] = true
		}
		counts[slug]++
	}
	return anchors
}

func documentationFiles(t *testing.T) []string {
	t.Helper()
	root := repositoryRoot(t)
	paths := []string{
		filepath.Join(root, "README.md"),
		filepath.Join(root, "SECURITY.md"),
		filepath.Join(root, "THREAT_MODEL.md"),
	}
	for _, dir := range []string{"docs", "deployments", "example"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() && strings.EqualFold(filepath.Ext(path), ".md") {
				paths = append(paths, path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return paths
}

func TestDocumentationUsesPlainASCII(t *testing.T) {
	for _, path := range documentationFiles(t) {
		text := readFile(t, path)
		for lineNumber, line := range strings.Split(text, "\n") {
			if strings.Contains(line, "**") {
				t.Errorf("%s:%d contains bold markup", path, lineNumber+1)
			}
			for _, r := range line {
				if r > unicode.MaxASCII {
					t.Errorf("%s:%d contains non-ASCII character %q", path, lineNumber+1, r)
				}
			}
		}
	}
}

func TestLocalDocumentationLinksResolve(t *testing.T) {
	for _, path := range documentationFiles(t) {
		text := readFile(t, path)
		for _, match := range markdownLink.FindAllStringSubmatch(text, -1) {
			destination := strings.TrimSpace(strings.Fields(match[1])[0])
			parsed, err := url.Parse(strings.Trim(destination, "<>"))
			if err != nil {
				t.Errorf("%s: parse link %q: %v", path, destination, err)
				continue
			}
			if parsed.IsAbs() {
				continue
			}
			target := path
			if parsed.Path != "" {
				target = filepath.Clean(filepath.Join(filepath.Dir(path), filepath.FromSlash(parsed.Path)))
			}
			if _, err := os.Stat(target); err != nil {
				t.Errorf("%s: link %q: %v", path, destination, err)
				continue
			}
			if parsed.Fragment != "" && strings.EqualFold(filepath.Ext(target), ".md") {
				if !markdownAnchors(readFile(t, target))[strings.ToLower(parsed.Fragment)] {
					t.Errorf("%s: link %q has no matching heading", path, destination)
				}
			}
		}
	}
}

func TestCompleteTOMLExamplesValidate(t *testing.T) {
	root := repositoryRoot(t)
	t.Setenv("TS_OAUTH_CLIENT_ID", "doccheck-client")
	t.Setenv("TS_OAUTH_CLIENT_SECRET", "doccheck-secret")

	tests := []struct {
		name    string
		path    string
		heading string
	}{
		{"readme", filepath.Join(root, "README.md"), "## Quick start"},
		{"quickstart", filepath.Join(root, "docs", "quickstart.md"), "## 2. Create Config File"},
		{"production", filepath.Join(root, "docs", "quickstart.md"), "### Production Setup"},
		{"reference", filepath.Join(root, "docs", "configuration-reference.md"), "## Complete Example"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text := readFile(t, tt.path)
			section := strings.SplitN(text, tt.heading, 2)
			if len(section) != 2 {
				t.Fatalf("heading %q not found", tt.heading)
			}
			block := regexp.MustCompile("(?s)```toml\\n(.*?)```").FindStringSubmatch(section[1])
			if len(block) != 2 {
				t.Fatalf("TOML block not found after %q", tt.heading)
			}
			path := filepath.Join(t.TempDir(), "tsbridge.toml")
			if err := os.WriteFile(path, []byte(block[1]), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := config.Load(path); err != nil {
				t.Fatalf("documented configuration does not validate: %v", err)
			}
		})
	}
}

func TestCheckedInTOMLExamplesValidate(t *testing.T) {
	root := repositoryRoot(t)
	t.Setenv("TS_OAUTH_CLIENT_ID", "doccheck-client")
	t.Setenv("TS_OAUTH_CLIENT_SECRET", "doccheck-secret")

	paths := []string{
		filepath.Join(root, "example", "simple", "tsbridge.toml"),
		filepath.Join(root, "example", "simple", "tsbridge-custom-ports.toml"),
		filepath.Join(root, "deployments", "freebsd", "config.example.toml"),
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			if _, err := config.Load(path); err != nil {
				t.Fatalf("configuration does not validate: %v", err)
			}
		})
	}
}

func TestDockerLabelReferenceCoversParser(t *testing.T) {
	root := repositoryRoot(t)
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "internal", "docker", "labels.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	docs := readFile(t, filepath.Join(root, "docs", "docker-labels.md"))
	getters := map[string]bool{
		"getString": true, "getBool": true, "getDuration": true,
		"getByteSize": true, "getStringSlice": true, "getHeaders": true,
	}
	keys := map[string]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !getters[selector.Sel.Name] {
			return true
		}
		object, ok := selector.X.(*ast.Ident)
		literal, literalOK := call.Args[0].(*ast.BasicLit)
		if !ok || object.Name != "parser" || !literalOK || literal.Kind != token.STRING {
			return true
		}
		key, err := strconv.Unquote(literal.Value)
		if err == nil {
			keys[key] = true
		}
		return true
	})
	for key := range keys {
		if !strings.Contains(docs, "`tsbridge."+key+"`") && !strings.Contains(docs, "`tsbridge."+key+".<name>`") {
			t.Errorf("Docker label reference is missing tsbridge.%s", key)
		}
	}
}

func TestCLIReferenceCoversFlags(t *testing.T) {
	root := repositoryRoot(t)
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "cmd", "tsbridge", "main.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	readme := readFile(t, filepath.Join(root, "README.md"))
	setters := map[string]bool{"StringVar": true, "BoolVar": true, "DurationVar": true}
	flags := map[string]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !setters[selector.Sel.Name] {
			return true
		}
		object, ok := selector.X.(*ast.Ident)
		literal, literalOK := call.Args[1].(*ast.BasicLit)
		if !ok || object.Name != "fs" || !literalOK || literal.Kind != token.STRING {
			return true
		}
		name, err := strconv.Unquote(literal.Value)
		if err == nil {
			flags[name] = true
		}
		return true
	})
	for name := range flags {
		pattern := regexp.MustCompile(`(^|[^A-Za-z0-9-])` + regexp.QuoteMeta("-"+name) + `([^A-Za-z0-9-]|$)`)
		if !pattern.MatchString(readme) {
			t.Errorf("CLI reference is missing -%s", name)
		}
	}
}

func TestComposeExamplesRender(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker CLI is not installed")
	}
	root := repositoryRoot(t)
	t.Setenv("TS_OAUTH_CLIENT_ID", "doccheck-client")
	t.Setenv("TS_OAUTH_CLIENT_SECRET", "doccheck-secret")
	t.Setenv("TS_AUTHKEY", "doccheck-key")

	tests := [][]string{
		{"-f", "example/simple/docker-compose.yml"},
		{"-f", "example/docker-labels/docker-compose.yml"},
		{"-f", "example/headscale/docker-compose.yml"},
		{"-f", "example/multi-compose/tsbridge-compose.yml", "-f", "example/multi-compose/services-compose.yml"},
	}
	for _, files := range tests {
		args := append([]string{"compose"}, files...)
		args = append(args, "config")
		command := exec.Command("docker", args...)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Errorf("docker %s: %v\n%s", strings.Join(args, " "), err, output)
		}
	}
}
