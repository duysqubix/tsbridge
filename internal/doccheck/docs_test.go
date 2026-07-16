package doccheck_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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

func configFieldKeys(value any) map[string]bool {
	keys := map[string]bool{}
	valueType := reflect.TypeOf(value)
	for i := 0; i < valueType.NumField(); i++ {
		if key := valueType.Field(i).Tag.Get("mapstructure"); key != "" {
			keys[key] = true
		}
	}
	return keys
}

func validateTOMLKeys(t *testing.T, path string) {
	t.Helper()
	sections := map[string]map[string]bool{
		"tailscale": configFieldKeys(config.Tailscale{}),
		"global":    configFieldKeys(config.Global{}),
		"services":  configFieldKeys(config.Service{}),
	}
	assignment := regexp.MustCompile(`^([a-z][a-z0-9_]*)\s*=`)
	section := ""
	for lineNumber, line := range strings.Split(readFile(t, path), "\n") {
		trimmed := strings.TrimSpace(line)
		switch trimmed {
		case "[tailscale]":
			section = "tailscale"
			continue
		case "[global]":
			section = "global"
			continue
		case "[[services]]":
			section = "services"
			continue
		}
		match := assignment.FindStringSubmatch(trimmed)
		if len(match) == 0 {
			continue
		}
		if !sections[section][match[1]] {
			t.Errorf("%s:%d contains unsupported %s key %q", path, lineNumber+1, section, match[1])
		}
	}
}

func setConfigEnvironment(t *testing.T) {
	t.Helper()
	for _, entry := range os.Environ() {
		key := strings.SplitN(entry, "=", 2)[0]
		if strings.HasPrefix(key, "TSBRIDGE_") || key == "TS_AUTHKEY" || key == "TS_AUTH_KEY" {
			t.Setenv(key, "")
		}
	}
	t.Setenv("TS_OAUTH_CLIENT_ID", "doccheck-client")
	t.Setenv("TS_OAUTH_CLIENT_SECRET", "doccheck-secret")
}

func markdownAnchors(text string) map[string]bool {
	anchors := map[string]bool{}
	counts := map[string]int{}
	inFence := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
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
		filepath.Join(root, "CHANGELOG.md"),
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
		inFence := false
		for lineNumber, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				inFence = !inFence
			}
			if !inFence && strings.Contains(line, "**") {
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

	tests := []struct {
		name       string
		path       string
		heading    string
		authKeyEnv bool
	}{
		{"readme", filepath.Join(root, "README.md"), "## Quick start", false},
		{"headscale", filepath.Join(root, "README.md"), "## Headscale", true},
		{"quickstart", filepath.Join(root, "docs", "quickstart.md"), "## 2. Create Config File", false},
		{"production", filepath.Join(root, "docs", "quickstart.md"), "### Production Setup", false},
		{"reference", filepath.Join(root, "docs", "configuration-reference.md"), "## Complete Example", false},
		{"freebsd", filepath.Join(root, "deployments", "freebsd", "README.md"), "### 3. Install Configuration", false},
		{"systemd", filepath.Join(root, "deployments", "systemd", "README.md"), "### Config File", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setConfigEnvironment(t)
			if tt.authKeyEnv {
				t.Setenv("TS_OAUTH_CLIENT_ID", "")
				t.Setenv("TS_OAUTH_CLIENT_SECRET", "")
				t.Setenv("TS_AUTH_KEY", "doccheck-key")
			}
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
			validateTOMLKeys(t, path)
			if _, err := config.Load(path); err != nil {
				t.Fatalf("documented configuration does not validate: %v", err)
			}
		})
	}
}

func TestCheckedInTOMLExamplesValidate(t *testing.T) {
	root := repositoryRoot(t)

	paths := []string{
		filepath.Join(root, "example", "simple", "tsbridge.toml"),
		filepath.Join(root, "example", "simple", "tsbridge-custom-ports.toml"),
		filepath.Join(root, "deployments", "freebsd", "config.example.toml"),
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			validateTOMLKeys(t, path)
			setConfigEnvironment(t)
			if _, err := config.Load(path); err != nil {
				t.Fatalf("configuration does not validate: %v", err)
			}
		})
	}
}

func implementationDockerLabelKeys(t *testing.T, root string) map[string]bool {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "internal", "docker", "labels.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	getters := map[string]bool{
		"getString": true, "getBool": true, "getDuration": true,
		"getByteSize": true, "getStringSlice": true, "getHeaders": true,
	}
	keys := map[string]bool{
		"enabled":      true,
		"enable":       true,
		"service.port": true,
	}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !getters[selector.Sel.Name] {
			return true
		}
		literal, ok := call.Args[0].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		key, err := strconv.Unquote(literal.Value)
		if err == nil {
			keys[key] = true
		}
		return true
	})
	// Accepted by the parser but not applied by the running process.
	delete(keys, "global.shutdown_timeout")
	return keys
}

func implementationCLIFlags(t *testing.T, root string) map[string]bool {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "cmd", "tsbridge", "main.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
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
		literal, ok := call.Args[1].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		name, err := strconv.Unquote(literal.Value)
		if err == nil {
			flags[name] = true
		}
		return true
	})
	return flags
}

func normalizeDockerLabelKey(key string) string {
	for _, dynamic := range []string{"service.upstream_headers", "service.downstream_headers"} {
		if strings.HasPrefix(key, dynamic+".") {
			return dynamic
		}
	}
	return key
}

type composeConfig struct {
	Services map[string]struct {
		Command []string          `json:"command"`
		Image   string            `json:"image"`
		Labels  map[string]string `json:"labels"`
	} `json:"services"`
}

func composeConfigErrors(rendered composeConfig, labelKeys, flags map[string]bool) []string {
	var errors []string
	for serviceName, service := range rendered.Services {
		for label := range service.Labels {
			if !strings.HasPrefix(label, "tsbridge.") {
				continue
			}
			key := normalizeDockerLabelKey(strings.TrimPrefix(label, "tsbridge."))
			if !labelKeys[key] {
				errors = append(errors, serviceName+" uses unsupported tsbridge label "+label)
			}
		}
		if !strings.Contains(service.Image, "tsbridge") {
			continue
		}
		for _, argument := range service.Command {
			if !strings.HasPrefix(argument, "-") {
				continue
			}
			name := strings.SplitN(strings.TrimLeft(argument, "-"), "=", 2)[0]
			if !flags[name] {
				errors = append(errors, serviceName+" uses unsupported tsbridge flag "+argument)
			}
		}
	}
	return errors
}

func yamlSnippetErrors(content string, labelKeys, flags map[string]bool) []string {
	var errors []string
	labelPattern := regexp.MustCompile(`tsbridge\.([A-Za-z0-9_]+(?:\.[A-Za-z0-9_-]+)*)`)
	for _, match := range labelPattern.FindAllStringSubmatch(content, -1) {
		key := normalizeDockerLabelKey(match[1])
		if !labelKeys[key] {
			errors = append(errors, "uses unsupported tsbridge label "+match[0])
		}
	}

	flagPattern := regexp.MustCompile(`(?:^|[^A-Za-z0-9_./])-{1,2}([A-Za-z][A-Za-z0-9-]*)`)
	for _, line := range strings.Split(content, "\n") {
		if !strings.Contains(line, "command:") {
			continue
		}
		if !strings.Contains(line, "docker") && !strings.Contains(line, "config") && !strings.Contains(line, "tsbridge") {
			continue
		}
		for _, match := range flagPattern.FindAllStringSubmatch(line, -1) {
			if !flags[match[1]] {
				errors = append(errors, "uses unsupported tsbridge flag "+match[1])
			}
		}
	}
	return errors
}

func TestDockerYAMLSnippetsMatchImplementation(t *testing.T) {
	root := repositoryRoot(t)
	labelKeys := implementationDockerLabelKeys(t, root)
	flags := implementationCLIFlags(t, root)
	yamlBlocks := regexp.MustCompile("(?s)```ya?ml\\n(.*?)```")
	for _, path := range documentationFiles(t) {
		for _, block := range yamlBlocks.FindAllStringSubmatch(readFile(t, path), -1) {
			for _, semanticError := range yamlSnippetErrors(block[1], labelKeys, flags) {
				t.Errorf("%s: %s", path, semanticError)
			}
		}
	}
}

func TestDockerLabelReferenceCoversParser(t *testing.T) {
	root := repositoryRoot(t)
	docs := readFile(t, filepath.Join(root, "docs", "docker-labels.md"))
	keys := implementationDockerLabelKeys(t, root)
	for key := range keys {
		exact := "| `tsbridge." + key + "`"
		dynamic := "| `tsbridge." + key + ".<name>`"
		if !strings.Contains(docs, exact) && !strings.Contains(docs, dynamic) {
			t.Errorf("Docker label reference is missing tsbridge.%s", key)
		}
	}
}

func TestCLIReferenceCoversFlags(t *testing.T) {
	root := repositoryRoot(t)
	readme := readFile(t, filepath.Join(root, "README.md"))
	flags := implementationCLIFlags(t, root)
	sections := strings.SplitN(readme, "## Command-line reference", 2)
	if len(sections) != 2 {
		t.Fatal("README is missing the command-line reference")
	}
	reference := strings.SplitN(sections[1], "## Configuration", 2)[0]
	for name := range flags {
		pattern := regexp.MustCompile(`(^|[^A-Za-z0-9-])` + regexp.QuoteMeta("-"+name) + `([^A-Za-z0-9-]|$)`)
		if !pattern.MatchString(reference) {
			t.Errorf("CLI reference is missing -%s", name)
		}
	}
}

func TestDockerOAuthExamplesIncludeTags(t *testing.T) {
	root := repositoryRoot(t)
	check := func(path, content string) {
		if !strings.Contains(content, "tailscale.oauth_client_secret_env") {
			return
		}
		if !strings.Contains(content, "tailscale.default_tags") && !strings.Contains(content, "service.tags") {
			t.Errorf("%s contains an OAuth Docker example without service tags", path)
		}
	}

	yamlBlocks := regexp.MustCompile("(?s)```ya?ml\\n(.*?)```")
	for _, path := range documentationFiles(t) {
		for _, block := range yamlBlocks.FindAllStringSubmatch(readFile(t, path), -1) {
			check(path, block[1])
		}
	}
	for _, relative := range []string{
		"example/docker-labels/docker-compose.yml",
		"example/multi-compose/tsbridge-compose.yml",
	} {
		path := filepath.Join(root, filepath.FromSlash(relative))
		check(path, readFile(t, path))
	}
}

func TestYAMLSnippetValidationRejectsUnsupportedKeys(t *testing.T) {
	content := `
services:
  tsbridge:
    command: ["--providre", "docker"]
    labels:
      - "tsbridge.service.poort=8080"
`
	errors := yamlSnippetErrors(
		content,
		map[string]bool{"service.port": true},
		map[string]bool{"provider": true},
	)
	if len(errors) != 2 {
		t.Fatalf("expected unsupported label and flag errors, got %v", errors)
	}
}

func TestComposeSemanticValidationRejectsUnsupportedKeys(t *testing.T) {
	var rendered composeConfig
	data := []byte(`{"services":{"tsbridge":{"image":"ghcr.io/jtdowney/tsbridge:latest","command":["--providre","docker"],"labels":{"tsbridge.service.poort":"8080"}}}}`)
	if err := json.Unmarshal(data, &rendered); err != nil {
		t.Fatal(err)
	}
	errors := composeConfigErrors(
		rendered,
		map[string]bool{"service.port": true},
		map[string]bool{"provider": true},
	)
	if len(errors) != 2 {
		t.Fatalf("expected unsupported label and flag errors, got %v", errors)
	}
}

func TestComposeExamplesRender(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker CLI is not installed")
	}
	probe := exec.Command("docker", "compose", "version")
	if err := probe.Run(); err != nil {
		t.Skip("Docker Compose plugin is not installed")
	}
	root := repositoryRoot(t)
	setConfigEnvironment(t)
	t.Setenv("TS_AUTHKEY", "doccheck-key")
	labelKeys := implementationDockerLabelKeys(t, root)
	flags := implementationCLIFlags(t, root)

	tests := [][]string{
		{"-f", "example/simple/docker-compose.yml"},
		{"-f", "example/docker-labels/docker-compose.yml"},
		{"-f", "example/headscale/docker-compose.yml"},
		{"-f", "example/multi-compose/tsbridge-compose.yml", "-f", "example/multi-compose/services-compose.yml"},
	}
	for _, files := range tests {
		args := append([]string{"compose"}, files...)
		args = append(args, "config", "--format", "json")
		command := exec.Command("docker", args...)
		command.Dir = root
		output, err := command.CombinedOutput()
		if err != nil {
			t.Errorf("docker %s: %v\n%s", strings.Join(args, " "), err, output)
			continue
		}
		var rendered composeConfig
		if err := json.Unmarshal(output, &rendered); err != nil {
			t.Errorf("decode docker %s output: %v", strings.Join(args, " "), err)
			continue
		}
		for _, semanticError := range composeConfigErrors(rendered, labelKeys, flags) {
			t.Errorf("%s: %s", strings.Join(files, " "), semanticError)
		}
	}
}
