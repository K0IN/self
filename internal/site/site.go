// Package site renders the public model registry (models/registry.yml plus
// the linked model cards) into a small static website for GitHub Pages:
//
//	index.html                 overview table of all models
//	models/<name>/<tag>.html   one page per model (rendered readme + quants)
//	registry.yml               the raw registry, for tooling
package site

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"

	"ai-server/internal/registry"
)

//go:embed templates/*.html
var templates embed.FS

// Options configure a build.
type Options struct {
	// Title of the site.
	Title string
	// RepoURL links back to the source repository ("" hides the link).
	RepoURL string
}

type fileView struct {
	Name, Role, SHA256, Size, URL string
}

type quantView struct {
	Name, Adapter, Repo, Size string
	Default                   bool
	Files                     []fileView
}

type modelView struct {
	ID, Name, Tag, Description, Type, Page, ReadmeSrc string
	Capabilities                                      []string
	DefaultSize                                       string
	Quants                                            []quantView
	Readme                                            template.HTML
	Info                                              []kv
	Settings                                          []kv
}

type kv struct{ K, V string }

func infoRows(i registry.Info) []kv {
	var out []kv
	add := func(k, v string) {
		if v != "" && v != "0" {
			out = append(out, kv{k, v})
		}
	}
	add("Family", i.Family)
	add("Parameters", i.Parameters)
	add("Architecture", i.Arch)
	add("Base model", i.BaseModel)
	add("Source", i.Source)
	add("License", i.License)
	add("Languages", strings.Join(i.Languages, ", "))
	add("Context length", fmt.Sprint(i.ContextLength))
	add("Max options", fmt.Sprint(i.MaxOptions))
	add("Homepage", i.Homepage)
	return out
}

func settingRows(m map[string]any) []kv {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]kv, len(keys))
	for i, k := range keys {
		out[i] = kv{k, fmt.Sprint(m[k])}
	}
	return out
}

// Build renders the site into out. registryYAML is the raw registry file;
// readmes resolves readme paths (relative to the registry file).
func Build(registryYAML []byte, readmes fs.FS, out string, opt Options) error {
	reg, err := registry.Parse(registryYAML)
	if err != nil {
		return err
	}
	if opt.Title == "" {
		opt.Title = "self model registry"
	}
	tpl, err := template.ParseFS(templates, "templates/*.html")
	if err != nil {
		return err
	}
	md := goldmark.New(goldmark.WithExtensions(extension.GFM))

	var views []modelView
	for _, id := range reg.IDs() {
		m := reg.Models[id]
		src, err := fs.ReadFile(readmes, m.Readme)
		if err != nil {
			return fmt.Errorf("model %s: readme: %w", id, err)
		}
		var html bytes.Buffer
		if err := md.Convert(src, &html); err != nil {
			return fmt.Errorf("model %s: readme: %w", id, err)
		}
		name, tag, _ := strings.Cut(id, ":")
		if tag == "" {
			tag = "latest"
		}
		v := modelView{
			ID: id, Name: name, Tag: tag, Description: m.Description, Type: string(m.Type),
			Page: path.Join("models", name, tag+".html"), ReadmeSrc: m.Readme,
			DefaultSize: FormatBytes(m.Variants[m.Default].Size()),
			Readme:      template.HTML(html.String()), // rendered from our own repo content
			Info:        infoRows(m.Info),
			Settings:    settingRows(m.Settings),
		}
		for _, c := range m.Capabilities.List() {
			v.Capabilities = append(v.Capabilities, string(c))
		}
		for _, q := range m.Quants() {
			vv := m.Variants[q]
			qv := quantView{Name: q, Adapter: vv.Adapter, Repo: vv.Repo, Size: FormatBytes(vv.Size()), Default: q == m.Default}
			for _, f := range vv.Files {
				qv.Files = append(qv.Files, fileView{
					Name: f.Name, Role: string(f.Role), SHA256: f.SHA256, Size: FormatBytes(f.Size),
					URL: "https://huggingface.co/" + vv.Repo + "/blob/main/" + f.Name,
				})
			}
			v.Quants = append(v.Quants, qv)
		}
		views = append(views, v)
	}

	write := func(rel, name string, data any) error {
		p := filepath.Join(out, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		var b bytes.Buffer
		if err := tpl.ExecuteTemplate(&b, name, data); err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		return os.WriteFile(p, b.Bytes(), 0o644)
	}
	if err := write("index.html", "index.html", map[string]any{"Opt": opt, "Models": views, "Root": ""}); err != nil {
		return err
	}
	for _, v := range views {
		if err := write(v.Page, "model.html", map[string]any{"Opt": opt, "M": v, "Root": "../../"}); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(out, "registry.yml"), registryYAML, 0o644); err != nil {
		return err
	}
	// GitHub Pages: serve files as-is (no Jekyll processing).
	return os.WriteFile(filepath.Join(out, ".nojekyll"), nil, 0o644)
}

// FormatBytes renders a byte count like "1.19 GiB".
func FormatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	v := float64(n) / float64(div)
	if v >= 100 {
		return fmt.Sprintf("%.0f %ciB", v, "KMGTPE"[exp])
	}
	return fmt.Sprintf("%.2f %ciB", v, "KMGTPE"[exp])
}
