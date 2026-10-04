package registry

import (
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// reserved model keys; every other mapping key of a model is a quant variant.
var reservedModelKeys = map[string]bool{
	"type": true, "default": true, "capabilities": true, "max_images": true,
	"description": true, "readme": true, "info": true, "settings": true,
}

var canonicalIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*:[0-9]+(?:\.[0-9]+)?[bm]$`)

var legacyModelIDs = map[string]string{
	"gemma4:e4b":  "gemma4:4b",
	"clm:8b":      "clm-v0.1:8b",
	"nomic:1.5":   "nomic-embed-text-v1.5:137m",
	"nomic:2-moe": "nomic-embed-text-v2-moe:475m",
}

// LoadFile reads and parses a registry file.
func LoadFile(p string) (*Registry, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	r, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	return r, nil
}

// Parse parses registry YAML.
func Parse(data []byte) (*Registry, error) {
	var doc struct {
		Version int                  `yaml:"version"`
		Models  map[string]yaml.Node `yaml:"models"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}
	if doc.Version != 1 {
		return nil, fmt.Errorf("registry: unsupported version %d (want 1)", doc.Version)
	}
	reg := &Registry{Version: doc.Version, Models: map[string]Model{}}
	seenIDs := map[string]bool{}
	for rawID, node := range doc.Models {
		id := normalizeModelID(rawID)
		if seenIDs[id] {
			return nil, fmt.Errorf("registry: duplicate canonical model id %q", id)
		}
		seenIDs[id] = true
		if err := validateID(id); err != nil {
			return nil, fmt.Errorf("registry: model %q: %w", rawID, err)
		}
		t, known, err := peekType(&node)
		if err != nil {
			return nil, fmt.Errorf("registry: model %q: %w", rawID, err)
		}
		if !known {
			reg.Skipped = append(reg.Skipped, id)
			continue
		}
		m, err := parseModel(id, t, &node)
		if err != nil {
			return nil, fmt.Errorf("registry: model %q: %w", rawID, err)
		}
		reg.Models[id] = m
	}
	sort.Strings(reg.Skipped)
	return reg, nil
}

func normalizeModelID(id string) string {
	if canonical, ok := legacyModelIDs[id]; ok {
		return canonical
	}
	return id
}

// peekType reads only the type of a model, so entries of a type this build
// does not know are skipped without judging fields it cannot understand.
func peekType(node *yaml.Node) (t ModelType, known bool, err error) {
	if node.Kind != yaml.MappingNode {
		return "", false, fmt.Errorf("must be a mapping")
	}
	var head struct {
		Type string `yaml:"type"`
	}
	if err := node.Decode(&head); err != nil {
		return "", false, err
	}
	if head.Type == "" {
		return "", false, fmt.Errorf("type is required")
	}
	t = ModelType(head.Type)
	_, known = modelTypes[t]
	return t, known, nil
}

// validateID ensures ids map to safe, human-readable directory names.
func validateID(id string) error {
	name, tag, hasTag := strings.Cut(id, ":")
	if name == "" || (hasTag && tag == "") {
		return fmt.Errorf("id must look like name or name:tag")
	}
	if !canonicalIDRE.MatchString(id) {
		return fmt.Errorf("id must look like name:version-size, with a numeric b/m size tag")
	}
	for _, part := range []string{name, tag} {
		if strings.ContainsAny(part, `/\: `) || part == "." || part == ".." {
			return fmt.Errorf("invalid characters in id")
		}
	}
	return nil
}

func parseModel(id string, t ModelType, node *yaml.Node) (Model, error) {
	var head struct {
		Default      string `yaml:"default"`
		Capabilities struct {
			Input  []string `yaml:"input"`
			Output []string `yaml:"output"`
		} `yaml:"capabilities"`
		MaxImages   int            `yaml:"max_images"`
		Description string         `yaml:"description"`
		Readme      string         `yaml:"readme"`
		Info        Info           `yaml:"info"`
		Settings    map[string]any `yaml:"settings"`
	}
	if err := node.Decode(&head); err != nil {
		return Model{}, err
	}
	if err := head.Info.validate(); err != nil {
		return Model{}, fmt.Errorf("info: %w", err)
	}
	if err := validateSettingKeys(head.Settings); err != nil {
		return Model{}, err
	}
	head.Description = strings.TrimSpace(head.Description)
	if head.Description == "" {
		return Model{}, fmt.Errorf("description is required")
	}
	if strings.ContainsAny(head.Description, "\n\r") {
		return Model{}, fmt.Errorf("description must be a single line (use readme for details)")
	}
	if err := validateReadme(head.Readme); err != nil {
		return Model{}, err
	}
	caps, err := parseCapabilities(t, head.Capabilities.Input, head.Capabilities.Output, head.MaxImages)
	if err != nil {
		return Model{}, err
	}
	m := Model{ID: id, Type: t, Default: head.Default, Capabilities: caps, Variants: map[string]Variant{},
		Description: head.Description, Readme: head.Readme, Info: head.Info, Settings: head.Settings}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if reservedModelKeys[key] {
			continue
		}
		v, err := parseVariant(t, key, node.Content[i+1])
		if err != nil {
			return Model{}, fmt.Errorf("quant %q: %w", key, err)
		}
		m.Variants[key] = v
	}
	if len(m.Variants) == 0 {
		return Model{}, fmt.Errorf("no quant variants")
	}
	if m.Default == "" {
		if len(m.Variants) != 1 {
			return Model{}, fmt.Errorf("default quant is required when more than one variant exists")
		}
		for q := range m.Variants {
			m.Default = q
		}
	}
	if _, ok := m.Variants[m.Default]; !ok {
		return Model{}, fmt.Errorf("default quant %q is not defined", m.Default)
	}
	return m, nil
}

func parseVariant(t ModelType, quant string, node *yaml.Node) (Variant, error) {
	if strings.ContainsAny(quant, `/\`) || quant == "." || quant == ".." {
		return Variant{}, fmt.Errorf("invalid quant name")
	}
	var raw struct {
		Adapter  string         `yaml:"adapter"`
		Repo     string         `yaml:"repo"`
		Files    []fileEntry    `yaml:"files"`
		Settings map[string]any `yaml:"settings"`
	}
	if err := node.Decode(&raw); err != nil {
		return Variant{}, err
	}
	if err := validateSettingKeys(raw.Settings); err != nil {
		return Variant{}, err
	}
	if raw.Adapter == "" {
		return Variant{}, fmt.Errorf("adapter is required")
	}
	if raw.Repo == "" || strings.Count(raw.Repo, "/") != 1 {
		return Variant{}, fmt.Errorf("repo must look like owner/name")
	}
	if len(raw.Files) == 0 {
		return Variant{}, fmt.Errorf("files must list at least one file")
	}
	v := Variant{Quant: quant, Adapter: raw.Adapter, Repo: raw.Repo, Settings: raw.Settings}
	roles := map[FileRole]bool{}
	for i, f := range raw.Files {
		if f.Name == "" {
			return Variant{}, fmt.Errorf("files[%d]: empty file name", i)
		}
		clean := path.Clean(f.Name)
		if clean != f.Name || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || strings.Contains(clean, `\`) {
			return Variant{}, fmt.Errorf("files[%d]: unsafe path %q", i, f.Name)
		}
		role := f.Role
		if role == "" {
			if i == 0 {
				role = RoleModel
			} else {
				return Variant{}, fmt.Errorf("files[%d]: role is required for additional files", i)
			}
		}
		if !slices.Contains(modelTypes[t].Roles, role) {
			return Variant{}, fmt.Errorf("files[%d]: unknown role %q for %s models", i, role, t)
		}
		ext := strings.ToLower(path.Ext(f.Name))
		switch {
		case role == RoleVoice:
			if ext != ".wav" && ext != ".mp3" {
				return Variant{}, fmt.Errorf("files[%d]: a voice must be a .wav or .mp3 file (%q)", i, f.Name)
			}
		case role == RoleHead:
			if ext != ".safetensors" {
				return Variant{}, fmt.Errorf("files[%d]: a head must be a .safetensors file (%q)", i, f.Name)
			}
		case t == TypeImage && (role == RoleVAE || role == RoleTextEncoder):
			if ext != ".gguf" && ext != ".safetensors" {
				return Variant{}, fmt.Errorf("files[%d]: image %s must be GGUF or safetensors (%q)", i, role, f.Name)
			}
		case ext != ".gguf":
			return Variant{}, fmt.Errorf("files[%d]: only GGUF files are supported (%q)", i, f.Name)
		}
		if f.Repo != "" && strings.Count(f.Repo, "/") != 1 {
			return Variant{}, fmt.Errorf("files[%d]: repo must look like owner/name", i)
		}
		if roles[role] {
			return Variant{}, fmt.Errorf("files[%d]: more than one file with role %s", i, role)
		}
		roles[role] = true
		if f.Size <= 0 {
			return Variant{}, fmt.Errorf("files[%d] %s: size (bytes) is required", i, f.Name)
		}
		sum := strings.ToLower(f.SHA256)
		if !isSHA256(sum) {
			return Variant{}, fmt.Errorf("files[%d] %s: sha256 must be 64 hex characters", i, f.Name)
		}
		v.Files = append(v.Files, File{Name: f.Name, Role: role, Repo: f.Repo, Revision: f.Revision, Size: f.Size, SHA256: sum})
	}
	if !roles[RoleModel] {
		return Variant{}, fmt.Errorf("exactly one file with role model is required")
	}
	return v, nil
}

var settingKeyRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// validateSettingKeys checks the shape of a settings map. Types and bounds
// are checked against the adapter's schema when the model is resolved.
func validateSettingKeys(s map[string]any) error {
	for k, v := range s {
		if !settingKeyRE.MatchString(k) {
			return fmt.Errorf("settings: invalid key %q (use snake_case)", k)
		}
		switch v.(type) {
		case int, int64, uint64, float64, bool, string:
		default:
			return fmt.Errorf("settings: %s must be a number, bool or string", k)
		}
	}
	return nil
}

// validateReadme requires a clean relative path to a Markdown file (relative
// to the registry file, e.g. readmes/kev/4b.md).
func validateReadme(p string) error {
	if p == "" {
		return fmt.Errorf("readme is required (path to a Markdown file, e.g. readmes/<name>/<tag>.md)")
	}
	if path.Clean(p) != p || path.IsAbs(p) || strings.HasPrefix(p, "../") || strings.Contains(p, `\`) || strings.Contains(p, "://") {
		return fmt.Errorf("readme %q must be a clean relative path next to the registry", p)
	}
	if !strings.HasSuffix(strings.ToLower(p), ".md") {
		return fmt.Errorf("readme %q must be a .md file", p)
	}
	return nil
}

func isSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// fileEntry is {file, role?, repo?, revision?, size, sha256}.
type fileEntry struct {
	Name     string
	Role     FileRole
	Repo     string
	Revision string
	Size     int64
	SHA256   string
}

func (f *fileEntry) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: file entry must be a mapping {file, size, sha256[, role, repo]}", n.Line)
	}
	var m struct {
		File     string `yaml:"file"`
		Role     string `yaml:"role"`
		Repo     string `yaml:"repo"`
		Revision string `yaml:"revision"`
		Size     int64  `yaml:"size"`
		SHA256   string `yaml:"sha256"`
	}
	if err := n.Decode(&m); err != nil {
		return err
	}
	f.Name, f.Role, f.Repo, f.Revision, f.Size, f.SHA256 = m.File, FileRole(m.Role), m.Repo, m.Revision, m.Size, m.SHA256
	return nil
}
