package ggmlclaya

import (
	"encoding/json"
	"fmt"

	"ai-server/internal/ggufmeta"
)

// Engine is the bundled executable this adapter drives.
const Engine = "laya"

// defaultMaxOptions matches the upstream compile default (MAX_OPTS = 16).
const defaultMaxOptions = 16

// CheckModel verifies that a GGUF can be executed by the Laya engine.
//
// The engine loads GGUFs through ggmlc's ModelLoader, which requires an
// embedded compiled graph (ggmlc.graph_spec). Decision preprocessing is read
// from ggmlc.decision; files without it are only valid if they are
// distributed Laya checkpoints (laya.* metadata). Anything else — notably
// llama.cpp-style GGUFs (general.architecture=qwen35, llama, ...) — cannot be
// executed and is rejected with an explanation instead of failing at load.
func CheckModel(md ggufmeta.Metadata) error {
	arch := ggufmeta.String(md, "general.architecture")
	if !ggufmeta.Has(md, "ggmlc.graph_spec") {
		return fmt.Errorf("the GGUF has general.architecture=%q and no embedded ggmlc graph (ggmlc.graph_spec). "+
			"The ggmlc/Laya decision engine only executes ggmlc-compiled decision GGUFs; "+
			"llama.cpp-style checkpoints must be compiled with ggmlc (with a ggmlc.decision recipe) first", arch)
	}
	if !ggufmeta.Has(md, "ggmlc.decision") && !ggufmeta.Has(md, "laya.model_name") && !ggufmeta.Has(md, "laya.family") {
		return fmt.Errorf("the GGUF contains a ggmlc graph but no ggmlc.decision recipe; it is not a decision model the engine can interpret")
	}
	return nil
}

// maxOptions reads the per-question option limit baked into the GGUF.
// The upstream engine aborts (not errors) on overflow, so the adapter must
// enforce it before sending a request.
func maxOptions(md ggufmeta.Metadata) int {
	if s := ggufmeta.String(md, "ggmlc.decision"); s != "" {
		var r struct {
			MaxOpts int `json:"max_opts"`
		}
		if json.Unmarshal([]byte(s), &r) == nil && r.MaxOpts > 0 {
			return r.MaxOpts
		}
	}
	for _, k := range []string{"laya.max_opts", "kev.max_opts"} {
		if v := ggufmeta.Int(md, k, 0); v > 0 {
			return v
		}
	}
	return defaultMaxOptions
}
