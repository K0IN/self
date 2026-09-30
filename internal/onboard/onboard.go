// Package onboard turns a Hugging Face GGUF repository into a registry entry.
//
// It lists the repository, groups GGUF files into quant buckets, detects a
// multimodal projector, reads the model header remotely (HTTP range requests,
// a few MiB) and picks the adapter from GGUF metadata. The result is a YAML
// snippet ready to paste into models/registry.yml — no Go code needed.
package onboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"ai-server/internal/ggufmeta"
)

// File is one repository file.
type File struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	Type string `json:"type"`
	LFS  *struct {
		OID  string `json:"oid"` // sha256 of the file content
		Size int64  `json:"size"`
	} `json:"lfs"`
}

// SHA256 returns the content hash published by the Hub (LFS files only).
func (f File) SHA256() string {
	if f.LFS == nil {
		return ""
	}
	return strings.ToLower(f.LFS.OID)
}

// Variant is one proposed quant.
type Variant struct {
	Quant  string
	Model  File
	MMProj *File
}

// Result is the analysis of a repository.
type Result struct {
	Repo         string
	ID           string
	Name         string // general.name from the GGUF
	Info         Info
	Adapter      string
	Arch         string
	Reason       string
	Capabilities []string
	Default      string
	Variants     []Variant
	Warnings     []string
}

// Client talks to the Hugging Face Hub.
type Client struct {
	HTTP    *http.Client
	BaseURL string
	Token   string
}

// NewClient honors HF_ENDPOINT and HF_TOKEN.
func NewClient() *Client {
	base := os.Getenv("HF_ENDPOINT")
	if base == "" {
		base = "https://huggingface.co"
	}
	return &Client{HTTP: &http.Client{Timeout: 60 * time.Second}, BaseURL: strings.TrimRight(base, "/"), Token: os.Getenv("HF_TOKEN")}
}

func (c *Client) list(ctx context.Context, repo string) ([]File, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/models/%s/tree/main?recursive=1", c.BaseURL, repo), nil)
	if err != nil {
		return nil, err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("listing %s: HTTP %d (private or gated repos need HF_TOKEN)", repo, resp.StatusCode)
	}
	var files []File
	return files, json.NewDecoder(resp.Body).Decode(&files)
}

// quant buckets, most preferred pattern first within each bucket.
var buckets = []struct {
	name     string
	patterns []*regexp.Regexp
}{
	{"4bit", res(`(?i)(^|[._-])(ud[._-])?q4_k_m([._-]|$)`, `(?i)(^|[._-])q4_k_s([._-]|$)`, `(?i)(^|[._-])iq4_xs([._-]|$)`, `(?i)(^|[._-])q4_0([._-]|$)`)},
	{"5bit", res(`(?i)(^|[._-])q5_k_m([._-]|$)`)},
	{"6bit", res(`(?i)(^|[._-])q6_k([._-]|$)`)},
	{"8bit", res(`(?i)(^|[._-])q8_0([._-]|$)`)},
	{"f16", res(`(?i)(^|[._-])(f16|bf16)([._-]|$)`)},
}

func res(ss ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(ss))
	for i, s := range ss {
		out[i] = regexp.MustCompile(s)
	}
	return out
}

var mmprojRe = regexp.MustCompile(`(?i)mmproj`)
var splitRe = regexp.MustCompile(`(?i)-\d{5}-of-\d{5}\.gguf$`)
var sizeRe = regexp.MustCompile(`^(?:\d+(?:\.\d+)?)[bmk]$`)

// DeriveID turns "owner/decider-2b-vision-GGUF" into "decider-vision:2b".
func DeriveID(repo string) string {
	name := repo[strings.LastIndexByte(repo, '/')+1:]
	name = regexp.MustCompile(`(?i)[-_.]?gguf$`).ReplaceAllString(name, "")
	name = strings.ToLower(name)
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' })
	for i, part := range parts {
		if sizeRe.MatchString(part) {
			modelParts := append([]string{}, parts[:i]...)
			modelParts = append(modelParts, parts[i+1:]...)
			if len(modelParts) > 0 {
				return strings.Join(modelParts, "-") + ":" + part
			}
		}
	}
	return name + ":latest"
}

// Analyze inspects a repository.
func (c *Client) Analyze(ctx context.Context, repo, id string) (*Result, error) {
	files, err := c.list(ctx, repo)
	if err != nil {
		return nil, err
	}
	if id == "" {
		id = DeriveID(repo)
	}
	r := &Result{Repo: repo, ID: id}
	var models, projs []File
	for _, f := range files {
		if f.Type != "file" || !strings.HasSuffix(strings.ToLower(f.Path), ".gguf") {
			continue
		}
		if splitRe.MatchString(f.Path) {
			r.Warnings = append(r.Warnings, "skipped split file "+f.Path+" (multi-part GGUFs are not supported yet)")
			continue
		}
		if mmprojRe.MatchString(f.Path) {
			projs = append(projs, f)
		} else {
			models = append(models, f)
		}
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("%s contains no GGUF model files", repo)
	}
	mmproj := pickMMProj(projs)

	used := map[string]bool{}
	for _, b := range buckets {
		for _, re := range b.patterns {
			var hit *File
			for i := range models {
				if !used[models[i].Path] && re.MatchString(models[i].Path) {
					hit = &models[i]
					break
				}
			}
			if hit != nil {
				used[hit.Path] = true
				r.Variants = append(r.Variants, Variant{Quant: b.name, Model: *hit, MMProj: mmproj})
				break
			}
		}
	}
	if len(r.Variants) == 0 {
		// Unrecognised naming: offer the smallest file as "default".
		sort.Slice(models, func(i, j int) bool { return models[i].Size < models[j].Size })
		r.Variants = append(r.Variants, Variant{Quant: "default", Model: models[0], MMProj: mmproj})
		r.Warnings = append(r.Warnings, "no known quant names found; using the smallest file")
	}
	r.Default = r.Variants[0].Quant
	for _, v := range r.Variants {
		for _, f := range []*File{&v.Model, v.MMProj} {
			if f != nil && (f.SHA256() == "" || f.LFS.Size <= 0) {
				r.Warnings = append(r.Warnings, "no LFS sha256 published for "+f.Path+"; fill in size/sha256 manually")
			}
		}
	}

	md, err := ggufmeta.ReadRemote(ctx, c.HTTP, fmt.Sprintf("%s/%s/resolve/main/%s", c.BaseURL, repo, r.Variants[0].Model.Path), c.Token)
	if err != nil {
		return nil, fmt.Errorf("reading GGUF header: %w", err)
	}
	r.Arch = ggufmeta.String(md, "general.architecture")
	r.Name = ggufmeta.String(md, "general.name")
	if r.Name == "" {
		r.Name = r.ID
	}
	r.Adapter, r.Reason = pickAdapter(md, mmproj != nil)
	r.Info = infoFrom(md)
	r.Capabilities = []string{"text", "choice", "score", "noul"}
	if mmproj != nil && r.Adapter == "ggmlc-custom-decider" {
		r.Capabilities = []string{"text", "vision", "choice", "score", "noul"}
	}
	if mmproj != nil && r.Adapter != "ggmlc-custom-decider" {
		r.Warnings = append(r.Warnings, "repository has an mmproj file but adapter "+r.Adapter+" has no image input; vision disabled")
		for i := range r.Variants {
			r.Variants[i].MMProj = nil
		}
	}
	return r, nil
}

func pickMMProj(projs []File) *File {
	if len(projs) == 0 {
		return nil
	}
	pref := []string{"q8_0", "f16", "bf16"}
	for _, p := range pref {
		for i := range projs {
			if strings.Contains(strings.ToLower(projs[i].Path), p) {
				return &projs[i]
			}
		}
	}
	return &projs[0]
}

// pickAdapter decides which engine can run the file, from metadata only.
func pickAdapter(md ggufmeta.Metadata, hasMMProj bool) (string, string) {
	arch := ggufmeta.String(md, "general.architecture")
	switch {
	case ggufmeta.Has(md, "ggmlc.graph_spec") && (ggufmeta.Has(md, "ggmlc.decision") || ggufmeta.Has(md, "laya.family") || ggufmeta.Has(md, "laya.model_name")):
		return "ggmlc-laya", "ggmlc-compiled graph with a decision recipe (ggmlc.decision / laya.*)"
	case ggufmeta.Has(md, "ggmlc.graph_spec"):
		return "ggmlc-laya", "ggmlc-compiled graph WITHOUT a decision recipe — probably not a decision model, verify with `self check`"
	case arch != "" && arch != "clip":
		return "ggmlc-custom-decider", fmt.Sprintf("llama.cpp checkpoint (architecture %q); valid for Decider-style one-pass models — verify with `self check`", arch)
	}
	return "", "unknown GGUF layout"
}

// YAML renders the registry entry, including the size/sha256 pins and a
// description/readme skeleton to edit.
func (r *Result) YAML() string {
	var b strings.Builder
	vision := ""
	for _, c := range r.Capabilities {
		if c == "vision" {
			vision = " (text + image)"
		}
	}
	fmt.Fprintf(&b, "  # onboarded from %s (architecture %s)\n", r.Repo, r.Arch)
	fmt.Fprintf(&b, "  %s:\n", r.ID)
	fmt.Fprintf(&b, "    description: %q\n", fmt.Sprintf("%s: decision model%s — TODO one-line summary", r.Name, vision))
	fmt.Fprintf(&b, "    readme: %s\n", r.ReadmePath())
	fmt.Fprintf(&b, "    type: decision\n    default: %s\n", r.Default)
	input, output := []string{"text"}, []string{"choice", "score", "noul"}
	for _, c := range r.Capabilities {
		if c == "vision" {
			input = append(input, c)
		}
	}
	fmt.Fprintf(&b, "    capabilities:\n      input: [%s]\n      output: [%s]\n", strings.Join(input, ", "), strings.Join(output, ", "))
	if lines := r.Info.lines(); len(lines) > 0 {
		b.WriteString("    info:\n")
		for _, l := range lines {
			fmt.Fprintf(&b, "      %s\n", l)
		}
	}
	if r.Adapter == "ggmlc-custom-decider" {
		b.WriteString("    settings:\n      context_size: 8192\n      temperature: 1.0\n")
	}
	for _, v := range r.Variants {
		fmt.Fprintf(&b, "    %s:\n      adapter: %s\n      repo: %s\n      files:\n", v.Quant, r.Adapter, r.Repo)
		b.WriteString(fileLine(v.Model, ""))
		if v.MMProj != nil {
			b.WriteString(fileLine(*v.MMProj, "mmproj"))
		}
	}
	return b.String()
}

// Info is registry info prefilled from GGUF metadata.
type Info struct {
	Parameters, Arch, BaseModel, Source, License string
	Languages                                    []string
	ContextLength, MaxOptions                    int
}

// infoFrom reads general.* plus llama.cpp / ggmlc (kev.*, laya.*) keys.
func infoFrom(md ggufmeta.Metadata) Info {
	var in Info
	in.Parameters = ggufmeta.String(md, "general.size_label")
	in.License = ggufmeta.String(md, "general.license")
	if u := ggufmeta.String(md, "general.base_model.0.repo_url"); strings.HasPrefix(u, "https://huggingface.co/") {
		in.BaseModel = strings.TrimPrefix(u, "https://huggingface.co/")
	}
	if u := ggufmeta.String(md, "general.source.url"); strings.HasPrefix(u, "https://huggingface.co/") {
		in.Source = strings.TrimPrefix(u, "https://huggingface.co/")
	}
	switch l := md["general.languages"].(type) {
	case []string:
		in.Languages = l
	case []any:
		for _, x := range l {
			if s, ok := x.(string); ok {
				in.Languages = append(in.Languages, s)
			}
		}
	}
	arch := ggufmeta.String(md, "general.architecture")
	in.Arch = arch
	for _, p := range []string{"kev", "laya"} {
		if a := ggufmeta.String(md, p+".arch"); a != "" {
			in.Arch = a
		}
		if b := ggufmeta.String(md, p+".base"); b != "" && in.BaseModel == "" {
			in.BaseModel = b
		}
		if c := ggufmeta.String(md, p+".checkpoint"); c != "" && in.Source == "" {
			in.Source = c
		}
		if n := ggufmeta.Int(md, p+".max_len", 0); n > 0 {
			in.ContextLength = n
		}
		if n := ggufmeta.Int(md, p+".max_opts", 0); n > 0 {
			in.MaxOptions = n
		}
	}
	if in.ContextLength == 0 && arch != "" {
		in.ContextLength = ggufmeta.Int(md, arch+".context_length", 0)
	}
	if in.MaxOptions == 0 && arch != "ggmlc" && arch != "" {
		in.MaxOptions = 10 // ggmlc-custom-decider lettered readout (A..J)
	}
	if in.Arch == "ggmlc" {
		in.Arch = ""
	}
	return in
}

func (in Info) lines() []string {
	var out []string
	add := func(k, v string) {
		if v != "" {
			out = append(out, fmt.Sprintf("%s: %q", k, v))
		}
	}
	add("parameters", in.Parameters)
	add("architecture", in.Arch)
	add("base_model", in.BaseModel)
	add("source", in.Source)
	add("license", in.License)
	if len(in.Languages) > 0 {
		out = append(out, "languages: ["+strings.Join(in.Languages, ", ")+"]")
	}
	if in.ContextLength > 0 {
		out = append(out, fmt.Sprintf("context_length: %d", in.ContextLength))
	}
	if in.MaxOptions > 0 {
		out = append(out, fmt.Sprintf("max_options: %d", in.MaxOptions))
	}
	return out
}

// ReadmePath is the model card location relative to models/registry.yml.
func (r *Result) ReadmePath() string {
	name, tag, _ := strings.Cut(r.ID, ":")
	if tag == "" {
		tag = "latest"
	}
	return "readmes/" + name + "/" + tag + ".md"
}

// Readme renders a model card skeleton for ReadmePath.
func (r *Result) Readme() string {
	var b strings.Builder
	input, output := []string{"text"}, []string{"choice", "score", "noul"}
	for _, c := range r.Capabilities {
		if c == "vision" {
			input = append(input, c)
		}
	}
	fmt.Fprintf(&b, "# %s\n\nTODO: what the model is good at, license, limits.\n\n", r.Name)
	fmt.Fprintf(&b, "- Source: [%s](https://huggingface.co/%s)\n", r.Repo, r.Repo)
	fmt.Fprintf(&b, "- Engine: `%s`\n- Input: %s\n- Output: %s\n\n", r.Adapter, strings.Join(input, ", "), strings.Join(output, ", "))
	fmt.Fprintf(&b, "```bash\nself serve %s\n```\n", r.ID)
	return b.String()
}

func fileLine(f File, role string) string {
	size, sum := f.Size, f.SHA256()
	if f.LFS != nil && f.LFS.Size > 0 {
		size = f.LFS.Size
	}
	if sum == "" {
		sum = "TODO"
	}
	s := fmt.Sprintf("        - {file: %s, size: %d, sha256: %s", f.Path, size, sum)
	if role != "" {
		s += ", role: " + role
	}
	return s + "}\n"
}
