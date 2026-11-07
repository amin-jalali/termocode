package containers

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Scaffold docker files (CTX-37): a small catalog of starter Dockerfile +
// compose.yaml + .dockerignore sets, compose service snippets, and the
// user's own templates (one folder per template). Files that already
// exist are never overwritten: the plan lists them as kept.

// Template is one starter set of files.
type Template struct {
	ID     string
	Title  string
	Custom bool              // from the user's template folder
	Files  map[string]string // relative path → content
}

// ScaffoldFile is one file of a plan.
type ScaffoldFile struct {
	Rel     string // relative to the root
	Path    string // absolute
	Content string
	Exists  bool // kept as it is
}

// ScaffoldPlan is what a template would write in root.
type ScaffoldPlan struct {
	Template Template
	Files    []ScaffoldFile // sorted by Rel
}

// Create lists the files that will be written.
func (p ScaffoldPlan) Create() []ScaffoldFile {
	var out []ScaffoldFile
	for _, f := range p.Files {
		if !f.Exists {
			out = append(out, f)
		}
	}
	return out
}

// Kept lists the files that exist and stay as they are.
func (p ScaffoldPlan) Kept() []ScaffoldFile {
	var out []ScaffoldFile
	for _, f := range p.Files {
		if f.Exists {
			out = append(out, f)
		}
	}
	return out
}

const dockerignore = `.git
.termocode
node_modules
__pycache__
*.log
.env
`

func composeFor(port string) string {
	return "services:\n  app:\n    build: .\n    ports:\n      - \"" + port + ":" + port + "\"\n"
}

// BuiltinTemplates is the starter catalog.
func BuiltinTemplates() []Template {
	return []Template{
		{ID: "go", Title: "Go (multi-stage, static binary)", Files: map[string]string{
			"Dockerfile": `FROM golang:1.23 AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/app .

FROM gcr.io/distroless/static-debian12 AS runtime
COPY --from=build /out/app /app
EXPOSE 8080
ENTRYPOINT ["/app"]
`,
			"compose.yaml": composeFor("8080"), ".dockerignore": dockerignore}},
		{ID: "node", Title: "Node.js", Files: map[string]string{
			"Dockerfile": `FROM node:22-alpine
WORKDIR /app
COPY package*.json ./
RUN npm ci --omit=dev
COPY . .
EXPOSE 3000
CMD ["npm", "start"]
`,
			"compose.yaml": composeFor("3000"), ".dockerignore": dockerignore}},
		{ID: "python", Title: "Python", Files: map[string]string{
			"Dockerfile": `FROM python:3.12-slim
WORKDIR /app
COPY requirements*.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
EXPOSE 8000
CMD ["python", "main.py"]
`,
			"compose.yaml": composeFor("8000"), ".dockerignore": dockerignore}},
		{ID: "static", Title: "Static site (nginx)", Files: map[string]string{
			"Dockerfile": `FROM nginx:alpine
COPY . /usr/share/nginx/html
EXPOSE 80
`,
			"compose.yaml": "services:\n  web:\n    build: .\n    ports:\n      - \"8080:80\"\n", ".dockerignore": dockerignore}},
		{ID: "generic", Title: "Generic (Alpine shell)", Files: map[string]string{
			"Dockerfile": `FROM alpine:3.20
WORKDIR /app
COPY . .
CMD ["sh"]
`,
			"compose.yaml": "services:\n  app:\n    build: .\n", ".dockerignore": dockerignore}},
	}
}

// DetectTemplate is the built-in template that fits root best.
func DetectTemplate(root string) string {
	has := func(n string) bool { _, err := os.Stat(filepath.Join(root, n)); return err == nil }
	switch {
	case has("go.mod"):
		return "go"
	case has("package.json"):
		return "node"
	case has("requirements.txt"), has("pyproject.toml"):
		return "python"
	case has("index.html"):
		return "static"
	}
	return "generic"
}

// maxTemplateFile caps one custom template file.
const maxTemplateFile = 256 << 10

// LoadCustomTemplates reads dir/<name>/… : each sub-folder is one
// template, its regular files (not in sub-folders, no links) are copied
// as they are. A missing dir is no templates.
func LoadCustomTemplates(dir string) ([]Template, error) {
	ents, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Template
	for _, e := range ents {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		files, err := os.ReadDir(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		t := Template{ID: "custom:" + e.Name(), Title: e.Name() + " (your template)", Custom: true, Files: map[string]string{}}
		for _, f := range files {
			if !f.Type().IsRegular() {
				continue
			}
			info, err := f.Info()
			if err != nil || info.Size() > maxTemplateFile {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, e.Name(), f.Name()))
			if err != nil {
				continue
			}
			t.Files[f.Name()] = string(b)
		}
		if len(t.Files) > 0 {
			out = append(out, t)
		}
	}
	return out, nil
}

// PlanScaffold is what t would write in root.
func PlanScaffold(root string, t Template) ScaffoldPlan {
	p := ScaffoldPlan{Template: t}
	for rel, content := range t.Files {
		path := filepath.Join(root, rel)
		_, err := os.Lstat(path)
		p.Files = append(p.Files, ScaffoldFile{Rel: rel, Path: path, Content: content, Exists: err == nil})
	}
	sort.Slice(p.Files, func(a, b int) bool { return p.Files[a].Rel < p.Files[b].Rel })
	return p
}

// WriteScaffold writes the plan's new files; an existing file is never
// touched (also when it appeared after the plan was made).
func WriteScaffold(p ScaffoldPlan) ([]string, error) {
	var wrote []string
	for _, f := range p.Create() {
		fh, err := os.OpenFile(f.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return wrote, err
		}
		_, werr := fh.WriteString(f.Content)
		if cerr := fh.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return wrote, werr
		}
		wrote = append(wrote, f.Rel)
	}
	return wrote, nil
}

// ComposeSnippet is one service of the compose template catalog.
type ComposeSnippet struct {
	ID, Title string
	Body      string // the service block, indented by two spaces, first line "  <id>:"
}

// ComposeSnippets is the service catalog.
func ComposeSnippets() []ComposeSnippet {
	return []ComposeSnippet{
		{ID: "postgres", Title: "PostgreSQL 16", Body: "  postgres:\n    image: postgres:16\n    environment:\n      POSTGRES_PASSWORD: postgres\n    ports:\n      - \"5432:5432\"\n    volumes:\n      - postgres-data:/var/lib/postgresql/data\n"},
		{ID: "mysql", Title: "MySQL 8", Body: "  mysql:\n    image: mysql:8\n    environment:\n      MYSQL_ROOT_PASSWORD: mysql\n    ports:\n      - \"3306:3306\"\n"},
		{ID: "redis", Title: "Redis 7", Body: "  redis:\n    image: redis:7\n    ports:\n      - \"6379:6379\"\n"},
		{ID: "mongo", Title: "MongoDB 7", Body: "  mongo:\n    image: mongo:7\n    ports:\n      - \"27017:27017\"\n"},
		{ID: "rabbitmq", Title: "RabbitMQ (with UI)", Body: "  rabbitmq:\n    image: rabbitmq:3-management\n    ports:\n      - \"5672:5672\"\n      - \"15672:15672\"\n"},
		{ID: "nginx", Title: "nginx", Body: "  nginx:\n    image: nginx:alpine\n    ports:\n      - \"8080:80\"\n"},
	}
}

// AddComposeService puts snippet s at the end of the services: block of
// a compose file's text ("" = a new file). It refuses a service name
// that is already there. Named volumes the snippet uses are not added
// to a top-level volumes: key that exists; a new file gets one.
func AddComposeService(text string, s ComposeSnippet) (string, error) {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if strings.TrimSpace(text) == "" {
		out := "services:\n" + s.Body
		if v := snippetVolume(s); v != "" {
			out += "\nvolumes:\n  " + v + ":\n"
		}
		return out, nil
	}
	for _, t := range composeTargets(lines) {
		if t.Service == s.ID {
			return "", fmt.Errorf("the compose file already has a service %q", s.ID)
		}
	}
	start := -1
	for i, l := range lines {
		if strings.TrimRight(l, " \t") == "services:" {
			start = i
			break
		}
	}
	if start < 0 {
		out := strings.Join(lines, "\n") + "\n\nservices:\n" + s.Body
		return out, nil
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		l := lines[i]
		if l != "" && l[0] != ' ' && l[0] != '\t' && !strings.HasPrefix(l, "#") {
			end = i
			break
		}
	}
	for end > start+1 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	body := reindent(s.Body, composeIndent(lines[start+1:end]))
	out := append([]string{}, lines[:end]...)
	out = append(out, strings.Split(strings.TrimRight(body, "\n"), "\n")...)
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n") + "\n", nil
}

func snippetVolume(s ComposeSnippet) string {
	for _, l := range strings.Split(s.Body, "\n") {
		t := strings.TrimSpace(l)
		if v, ok := strings.CutPrefix(t, "- "); ok {
			if name, _, ok := strings.Cut(v, ":/"); ok && !strings.ContainsAny(name, "./\"") {
				return name
			}
		}
	}
	return ""
}

// composeIndent is the indent of the first service key ("  " when none).
func composeIndent(block []string) string {
	for _, l := range block {
		if m := composeKeyRe.Regexp().FindStringSubmatch(l); m != nil {
			return m[1]
		}
	}
	return "  "
}

// reindent turns the two-space steps of body into steps of ind.
func reindent(body, ind string) string {
	if ind == "  " {
		return body
	}
	var b strings.Builder
	for _, l := range strings.Split(body, "\n") {
		n := len(l) - len(strings.TrimLeft(l, " "))
		b.WriteString(strings.Repeat(ind, n/2) + strings.TrimLeft(l, " ") + "\n")
	}
	return b.String()
}
