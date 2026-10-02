// clef: native engine for Cloudflare Clef Flash (Qwen3.5-VL 9B backbone plus a
// joint schema head). It speaks the same SELFIPC1 protocol as the other
// engines and never generates text:
//
//   1. the request is encoded exactly like the reference implementation
//      (joint_schema_model.py: encode_record): a chat-framed prompt with the
//      state, optional images and a "SCHEMA FIELDS" section;
//   2. the backbone (llama.cpp / mtmd, any GGUF quantisation) runs once and
//      returns the final hidden state of every position;
//   3. the joint head (head.hpp) turns those states into one logit per
//      allowed option of every question, jointly for all questions.
//
// Transport: SELFIPC1 frames on stdin/stdout (see internal/ipc in the Go
// server). stdout carries protocol frames only; every log goes to stderr.
//
//   frame := "SELFIPC1" | be32 header_len | header JSON | be32 n | (be64 len | bytes)*n
//
// Handshake (engine -> server, first frame):
//   {"type":"ready","protocol":1,"model":...,"device":...,"capabilities":{...}}
// Request:  {"id":N,"method":"systemone","params":{"state":<json>,
//            "questions":[{"id","type","instructions","criteria"}...],
//            "images":[{"attachment":0,"width":W,"height":H,"format":"rgb8"}]}}
// Response: {"id":N,"result":{"answers":{...},"usage":{...}}}
//        or {"id":N,"error":{"type":"invalid_request","message":"..."}}

#include "head.hpp"

#include "ggml-backend.h"
#include "gguf.h"
#include "llama.h"
#include "mtmd-helper.h"
#include "mtmd.h"

#include <nlohmann/json.hpp>

#include <algorithm>
#include <chrono>
#include <cmath>
#include <cstdarg>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <memory>
#include <set>
#include <stdexcept>
#include <string>
#include <unistd.h>
#include <vector>

using ojson = nlohmann::ordered_json;
using sjson = nlohmann::json; // keys sorted: Python's sort_keys=True

namespace {

constexpr int kMaxOptions = 32;
constexpr int kMaxImages = 4;
constexpr int kBatch = 512; // tokens per decode; keeps the per-position logits buffer small

// ---------------------------------------------------------------- logging

bool g_verbose = false;

void log_cb(enum ggml_log_level level, const char* text, void*) {
    if (g_verbose || level >= GGML_LOG_LEVEL_WARN) fputs(text, stderr);
}

void logf(const char* fmt, ...) __attribute__((format(printf, 1, 2)));
void logf(const char* fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    fputs("[clef] ", stderr);
    vfprintf(stderr, fmt, ap);
    fputc('\n', stderr);
    va_end(ap);
}

// ---------------------------------------------------------------- framing

constexpr char kMagic[8] = {'S', 'E', 'L', 'F', 'I', 'P', 'C', '1'};
constexpr uint32_t kMaxHeader = 4u << 20;
constexpr uint32_t kMaxAttachments = 32;
constexpr uint64_t kMaxAttachment = 128ull << 20;

struct Frame {
    std::string header;
    std::vector<std::vector<uint8_t>> attachments;
};

bool read_exact(void* dst, size_t n) {
    return fread(dst, 1, n, stdin) == n;
}

uint32_t be32(const uint8_t* b) {
    return (uint32_t(b[0]) << 24) | (uint32_t(b[1]) << 16) | (uint32_t(b[2]) << 8) | uint32_t(b[3]);
}

uint64_t be64(const uint8_t* b) {
    return (uint64_t(be32(b)) << 32) | be32(b + 4);
}

// 1 = frame, 0 = clean EOF, throws on protocol error.
int read_frame(Frame& f) {
    uint8_t hdr[12];
    size_t got = fread(hdr, 1, sizeof(hdr), stdin);
    if (got == 0 && feof(stdin)) return 0;
    if (got != sizeof(hdr)) throw std::runtime_error("truncated frame");
    if (memcmp(hdr, kMagic, 8) != 0) throw std::runtime_error("bad frame magic");
    uint32_t hlen = be32(hdr + 8);
    if (hlen == 0 || hlen > kMaxHeader) throw std::runtime_error("bad header length");
    f.header.resize(hlen);
    if (!read_exact(f.header.data(), hlen)) throw std::runtime_error("truncated header");
    uint8_t nb[8];
    if (!read_exact(nb, 4)) throw std::runtime_error("truncated attachment count");
    uint32_t n = be32(nb);
    if (n > kMaxAttachments) throw std::runtime_error("too many attachments");
    f.attachments.assign(n, {});
    for (uint32_t i = 0; i < n; ++i) {
        if (!read_exact(nb, 8)) throw std::runtime_error("truncated attachment length");
        uint64_t len = be64(nb);
        if (len > kMaxAttachment) throw std::runtime_error("attachment too large");
        f.attachments[i].resize(len);
        if (len && !read_exact(f.attachments[i].data(), len)) throw std::runtime_error("truncated attachment");
    }
    return 1;
}

void put32(std::string& o, uint32_t v) {
    for (int s = 24; s >= 0; s -= 8) o.push_back(char((v >> s) & 0xff));
}

void write_frame(const ojson& header) {
    std::string h = header.dump(-1, ' ', false, ojson::error_handler_t::replace);
    std::string out(kMagic, 8);
    put32(out, uint32_t(h.size()));
    out += h;
    put32(out, 0);
    fwrite(out.data(), 1, out.size(), stdout);
    fflush(stdout);
}

struct BadRequest : std::runtime_error {
    using std::runtime_error::runtime_error;
};

// ------------------------------------------------- reference rendering

const char kSystemPrompt[] =
    "Read the complete state and schema. Decide every field jointly. Each answer "
    "must be exactly one of that field's allowed options.";

sjson sorted(const ojson& j) {
    return sjson::parse(j.dump(-1, ' ', false, ojson::error_handler_t::replace));
}

// render(): strings as they are, anything else as compact JSON with sorted keys.
std::string render(const ojson& v) {
    if (v.is_string()) return v.get<std::string>();
    return sorted(v).dump(-1, ' ', false, sjson::error_handler_t::replace);
}

struct Option {
    std::string id;
    ojson description; // null: none
};

struct Question {
    std::string id, type, instructions;
    int type_index = 0;
    std::vector<Option> options;      // schema order (what the head scores)
    std::vector<std::string> request; // option ids in request order (answer order)
};

std::vector<Question> parse_questions(const ojson& questions) {
    if (!questions.is_array() || questions.empty()) throw BadRequest("questions must be a non-empty array");
    std::vector<Question> out;
    for (const auto& q : questions) {
        if (!q.is_object()) throw BadRequest("every question must be an object");
        Question p;
        p.id = q.value("id", "");
        p.type = q.value("type", "");
        if (p.id.empty()) throw BadRequest("question without id");
        const ojson crit = q.contains("criteria") ? q["criteria"] : ojson();
        const ojson ins = q.contains("instructions") ? q["instructions"] : ojson();
        p.instructions = (ins.is_null() || (ins.is_string() && ins.get<std::string>().empty())) ? p.id : render(ins);

        if (p.type == "noul") {
            p.type_index = 0;
            ojson d = {{"true", "The proposition is true or the answer is yes."},
                       {"false", "The proposition is false or the answer is no."}};
            if (crit.is_object())
                for (auto it = crit.begin(); it != crit.end(); ++it) d[it.key()] = it.value();
            p.options = {{"true", d["true"]}, {"false", d["false"]}};
            p.request = {"true", "false"};
        } else if (p.type == "choice") {
            p.type_index = 1;
            if (crit.is_object()) {
                for (auto it = crit.begin(); it != crit.end(); ++it) p.options.push_back({it.key(), it.value()});
            } else if (crit.is_array()) {
                for (const auto& n : crit) p.options.push_back({render(n), ojson()});
            }
            if (p.options.empty()) throw BadRequest("question " + p.id + ": choice criteria are required");
            for (const auto& o : p.options) p.request.push_back(o.id);
            std::set<std::string> seen(p.request.begin(), p.request.end());
            if (seen.size() != p.request.size()) throw BadRequest("question " + p.id + ": duplicate options");
            std::stable_sort(p.options.begin(), p.options.end(), [](const Option& a, const Option& b) { return a.id < b.id; });
        } else if (p.type == "score") {
            p.type_index = 2;
            if (!crit.is_array() || crit.empty()) throw BadRequest("question " + p.id + ": score criteria are required");
            for (size_t i = 0; i < crit.size(); ++i) {
                p.options.push_back({std::to_string(i), crit[i]});
                p.request.push_back(std::to_string(i));
            }
        } else {
            throw BadRequest("question " + p.id + ": type must be noul, choice, or score");
        }
        if (int(p.options.size()) > kMaxOptions)
            throw BadRequest("question " + p.id + ": at most " + std::to_string(kMaxOptions) + " options are supported");
        out.push_back(std::move(p));
    }
    return out;
}

double r4(double x) {
    return std::round(x * 10000.0) / 10000.0;
}

// ------------------------------------------- output embedding (lexical)

// Rows of the backbone's output projection, read straight from the GGUF
// (they are only needed for the few tokens of each option).
class OutputEmbeddings {
public:
    OutputEmbeddings(const std::string& path, int64_t n_embd) : n_embd_(n_embd) {
        ggml_context* mctx = nullptr;
        gguf_init_params gp{true, &mctx};
        gguf_context* g = gguf_init_from_file(path.c_str(), gp);
        if (!g) throw std::runtime_error("cannot read GGUF metadata from " + path);
        const char* name = gguf_find_tensor(g, "output.weight") >= 0 ? "output.weight" : "token_embd.weight";
        int64_t id = gguf_find_tensor(g, name);
        ggml_tensor* t = id >= 0 ? ggml_get_tensor(mctx, name) : nullptr;
        if (!t) {
            gguf_free(g);
            if (mctx) ggml_free(mctx);
            throw std::runtime_error("GGUF has no output embedding matrix");
        }
        type_ = gguf_get_tensor_type(g, id);
        rows_ = t->ne[1];
        const int64_t ne0 = t->ne[0];
        offset_ = gguf_get_data_offset(g) + gguf_get_tensor_offset(g, id);
        gguf_free(g);
        ggml_free(mctx);
        if (ne0 != n_embd) throw std::runtime_error("output embedding width does not match the model");
        row_bytes_ = ggml_row_size(type_, ne0);
        traits_ = ggml_get_type_traits(type_);
        if (type_ != GGML_TYPE_F32 && (!traits_ || !traits_->to_float))
            throw std::runtime_error("unsupported output embedding type");

        fd_ = ::open(path.c_str(), O_RDONLY);
        if (fd_ < 0) throw std::runtime_error("cannot open " + path);
        struct stat st {};
        if (::fstat(fd_, &st) != 0) throw std::runtime_error("cannot stat " + path);
        size_ = size_t(st.st_size);
        if (offset_ + size_t(rows_) * row_bytes_ > size_) throw std::runtime_error("output embedding is out of range");
        void* p = ::mmap(nullptr, size_, PROT_READ, MAP_PRIVATE, fd_, 0);
        if (p == MAP_FAILED) throw std::runtime_error("cannot map " + path);
        base_ = static_cast<const uint8_t*>(p);
        ::madvise(p, size_, MADV_RANDOM);
    }

    ~OutputEmbeddings() {
        if (base_) ::munmap(const_cast<uint8_t*>(base_), size_);
        if (fd_ >= 0) ::close(fd_);
    }
    OutputEmbeddings(const OutputEmbeddings&) = delete;
    OutputEmbeddings& operator=(const OutputEmbeddings&) = delete;

    // out[n_embd] += row(token)
    void add_row(int32_t token, float* out) {
        if (token < 0 || int64_t(token) >= rows_) throw std::runtime_error("token outside the output embedding");
        const clef::Vec& r = row(token);
        for (int64_t i = 0; i < n_embd_; ++i) out[i] += r[size_t(i)];
    }

private:
    const clef::Vec& row(int32_t token) {
        auto it = cache_.find(token);
        if (it != cache_.end()) return it->second;
        if (cache_.size() >= 16384) cache_.clear();
        clef::Vec v(static_cast<size_t>(n_embd_));
        const uint8_t* src = base_ + offset_ + size_t(token) * row_bytes_;
        if (type_ == GGML_TYPE_F32) std::memcpy(v.data(), src, size_t(n_embd_) * sizeof(float));
        else traits_->to_float(src, v.data(), n_embd_);
        return cache_.emplace(token, std::move(v)).first->second;
    }

    int64_t n_embd_, rows_ = 0;
    ggml_type type_ = GGML_TYPE_F32;
    const ggml_type_traits* traits_ = nullptr;
    size_t offset_ = 0, row_bytes_ = 0, size_ = 0;
    int fd_ = -1;
    const uint8_t* base_ = nullptr;
    std::unordered_map<int32_t, clef::Vec> cache_;
};

// ---------------------------------------------------------------- engine

struct Options {
    std::string model, head, mmproj, device = "auto";
    int n_ctx = 16384;
    int threads = 0;
    int gpu_layers = -1;             // -1 = all
    std::string flash_attn = "auto"; // auto | on | off
    int image_min_tokens = 0;        // 0 = from mmproj metadata
    int image_max_tokens = 0;
};

// One contiguous run of input positions: text tokens or one image chunk.
struct Segment {
    std::vector<llama_token> tokens;
    const mtmd_input_chunk* image = nullptr;
    int64_t size() const { return image ? int64_t(mtmd_input_chunk_get_n_tokens(image)) : int64_t(tokens.size()); }
};

struct Capture {
    llama_context* ctx;
    float* dst;
    int64_t n_embd, rows, got;
};

int32_t capture_cb(const mtmd_helper_embd_batch* b, void* user) {
    auto* c = static_cast<Capture*>(user);
    const float* e = llama_get_embeddings(c->ctx);
    if (!e || c->got + b->n_tokens > c->rows) return 1;
    std::memcpy(c->dst + c->got * c->n_embd, e, size_t(b->n_tokens) * size_t(c->n_embd) * sizeof(float));
    c->got += b->n_tokens;
    return 0;
}

class Engine {
public:
    explicit Engine(const Options& o) : opt_(o) {}

    ~Engine() {
        if (batch_ok_) llama_batch_free(batch_);
        if (mtmd_) mtmd_free(mtmd_);
        if (ctx_) llama_free(ctx_);
        if (model_) llama_model_free(model_);
        llama_backend_free();
    }

    llama_flash_attn_type flash_type() const {
        if (opt_.flash_attn == "on") return LLAMA_FLASH_ATTN_TYPE_ENABLED;
        if (opt_.flash_attn == "off") return LLAMA_FLASH_ATTN_TYPE_DISABLED;
        return LLAMA_FLASH_ATTN_TYPE_AUTO;
    }

    void load() {
        llama_log_set(log_cb, nullptr);
        mtmd_helper_log_set(log_cb, nullptr);
        load_backends();
        llama_backend_init();

        threads_ = opt_.threads > 0 ? opt_.threads : int(std::max(1u, std::thread::hardware_concurrency()));

        bool cpu = opt_.device == "cpu";
        llama_model_params mp = llama_model_default_params();
        mp.n_gpu_layers = cpu ? 0 : opt_.gpu_layers;
        if (opt_.device.rfind("cuda:", 0) == 0 || opt_.device.rfind("vulkan:", 0) == 0) {
            mp.main_gpu = std::atoi(opt_.device.substr(opt_.device.find(':') + 1).c_str());
            mp.split_mode = LLAMA_SPLIT_MODE_NONE;
        }
        model_ = llama_model_load_from_file(opt_.model.c_str(), mp);
        if (!model_) throw std::runtime_error("cannot load model " + opt_.model);
        n_embd_ = llama_model_n_embd_out(model_);

        llama_context_params cp = llama_context_default_params();
        // Every position's hidden state is needed, so embeddings are enabled
        // without pooling. Decodes stay within kBatch positions: llama.cpp
        // also materialises vocabulary logits for every output position.
        cp.n_ctx = uint32_t(opt_.n_ctx);
        cp.n_batch = uint32_t(kBatch);
        cp.n_ubatch = uint32_t(kBatch);
        cp.n_seq_max = 1;
        cp.embeddings = true;
        cp.pooling_type = LLAMA_POOLING_TYPE_NONE;
        cp.flash_attn_type = flash_type();
        cp.n_threads = cp.n_threads_batch = threads_;
        ctx_ = llama_init_from_model(model_, cp);
        if (!ctx_) throw std::runtime_error("cannot create llama context");
        vocab_ = llama_model_get_vocab(model_);
        batch_ = llama_batch_init(kBatch, 0, 1);
        batch_ok_ = true;

        head_ = std::make_unique<clef::Head>(opt_.head, threads_);
        if (head_->hidden_size() != n_embd_) throw std::runtime_error("joint head does not match the backbone width");
        lexical_ = std::make_unique<OutputEmbeddings>(opt_.model, n_embd_);

        if (!opt_.mmproj.empty()) {
            mtmd_context_params mcp = mtmd_context_params_default();
            mcp.use_gpu = !cpu;
            mcp.print_timings = false;
            mcp.warmup = false;
            mcp.n_threads = threads_;
            mcp.flash_attn_type = flash_type();
            if (opt_.image_min_tokens > 0) mcp.image_min_tokens = opt_.image_min_tokens;
            if (opt_.image_max_tokens > 0) mcp.image_max_tokens = opt_.image_max_tokens;
            mtmd_ = mtmd_init_from_file(opt_.mmproj.c_str(), model_, mcp);
            if (!mtmd_) throw std::runtime_error("cannot load mmproj " + opt_.mmproj);
            if (!mtmd_support_vision(mtmd_)) throw std::runtime_error("mmproj has no vision encoder");
        }
    }

    ojson ready() const {
        char name[256] = {0};
        llama_model_meta_val_str(model_, "general.name", name, sizeof(name));
        ojson vision = {{"enabled", mtmd_ != nullptr}};
        if (mtmd_) {
            vision["max_images"] = kMaxImages;
            // mtmd does the exact Qwen-VL smart resize; the server only needs
            // to bound the size so no oversized pixels travel over the pipe.
            vision["input"] = {{"mode", "bounded"}, {"max_width", 1536}, {"max_height", 1536}, {"resize", "contain"}};
        }
        return ojson{{"type", "ready"},
                     {"protocol", 1},
                     {"model", name[0] ? name : "clef-flash"},
                     {"device", opt_.device},
                     {"capabilities",
                      {{"text", true},
                       {"choice", true},
                       {"score", true},
                       {"noul", true},
                       {"max_options", kMaxOptions},
                       {"vision", vision}}}};
    }

    ojson systemone(const ojson& params, const std::vector<std::vector<uint8_t>>& attachments) {
        const auto t0 = std::chrono::steady_clock::now();
        std::vector<Question> questions = parse_questions(params.contains("questions") ? params["questions"] : ojson());
        const ojson state = params.contains("state") ? params["state"] : ojson();
        const ojson images = params.contains("images") ? params["images"] : ojson::array();
        if (images.size() > size_t(kMaxImages)) throw BadRequest("at most " + std::to_string(kMaxImages) + " images per request are supported");
        if (!images.empty() && !mtmd_) throw BadRequest("this model has no vision encoder");

        // ---- schema section (positions relative to its first token)
        std::vector<llama_token> schema = tokenize("\n\nSCHEMA FIELDS:\n", false);
        struct Rel {
            int64_t q0, q1;
            std::vector<std::pair<int64_t, int64_t>> options;
        };
        std::vector<Rel> rel(questions.size());
        auto append = [&](const std::string& s) {
            auto t = tokenize(s, false);
            schema.insert(schema.end(), t.begin(), t.end());
        };
        for (size_t qi = 0; qi < questions.size(); ++qi) {
            const Question& q = questions[qi];
            append("\nFIELD " + std::to_string(qi + 1) + "\nID: " + q.id + "\nTYPE: " + q.type + "\nINSTRUCTION: ");
            rel[qi].q0 = int64_t(schema.size());
            append(q.instructions);
            rel[qi].q1 = int64_t(schema.size());
            append("\nALLOWED OPTIONS:\n");
            for (size_t oi = 0; oi < q.options.size(); ++oi) {
                append("OPTION " + std::to_string(oi + 1) + ": ");
                sjson semantics;
                semantics["option_id"] = q.options[oi].id;
                if (!q.options[oi].description.is_null()) semantics["description"] = sorted(q.options[oi].description);
                const int64_t s = int64_t(schema.size());
                append(semantics.dump(-1, ' ', false, sjson::error_handler_t::replace));
                rel[qi].options.push_back({s, int64_t(schema.size())});
                append("\n");
            }
            append("END FIELD\n");
        }

        // ---- prefix, images, state, suffix
        std::vector<llama_token> prefix = tokenize(std::string("<|im_start|>system\n") + kSystemPrompt +
                                                       "<|im_end|>\n<|im_start|>user\nSTATE:\n",
                                                   true);
        std::vector<llama_token> suffix =
            tokenize("\n<|im_end|>\n<|im_start|>assistant\n<think>\n\n</think>\n\nJOINT SCHEMA DECISIONS:", true);

        std::vector<std::unique_ptr<mtmd_bitmap, decltype(&mtmd_bitmap_free)>> bitmaps;
        std::vector<const mtmd_bitmap*> bitmap_ptrs;
        std::unique_ptr<mtmd_input_chunks, decltype(&mtmd_input_chunks_free)> chunks(mtmd_input_chunks_init(), mtmd_input_chunks_free);
        std::vector<Segment> head_segments; // everything before the state
        head_segments.emplace_back();
        head_segments.back().tokens = prefix;
        if (!images.empty()) {
            for (const auto& im : images) {
                size_t ai = im.value("attachment", size_t(0));
                int w = im.value("width", 0), h = im.value("height", 0);
                if (im.value("format", "") != "rgb8") throw BadRequest("image format must be rgb8");
                if (ai >= attachments.size() || w <= 0 || h <= 0 || attachments[ai].size() != size_t(w) * size_t(h) * 3)
                    throw BadRequest("image attachment does not match its width/height");
                bitmaps.emplace_back(mtmd_bitmap_init(uint32_t(w), uint32_t(h), attachments[ai].data()), mtmd_bitmap_free);
                bitmap_ptrs.push_back(bitmaps.back().get());
            }
            std::string media;
            for (size_t i = 0; i < images.size(); ++i) media += mtmd_default_marker();
            media += "\n";
            mtmd_input_text in{media.c_str(), media.size(), false, true};
            if (mtmd_tokenize(mtmd_, chunks.get(), &in, bitmap_ptrs.data(), bitmap_ptrs.size()) != 0)
                throw BadRequest("cannot preprocess the images");
            for (size_t i = 0; i < mtmd_input_chunks_size(chunks.get()); ++i) {
                const mtmd_input_chunk* c = mtmd_input_chunks_get(chunks.get(), i);
                if (mtmd_input_chunk_get_type(c) == MTMD_INPUT_CHUNK_TYPE_TEXT) {
                    size_t n = 0;
                    const llama_token* t = mtmd_input_chunk_get_tokens_text(c, &n);
                    auto& cur = head_segments.back().tokens; // text segment, never an image
                    cur.insert(cur.end(), t, t + n);
                } else if (mtmd_input_chunk_get_type(c) == MTMD_INPUT_CHUNK_TYPE_IMAGE) {
                    Segment s;
                    s.image = c;
                    head_segments.push_back(std::move(s));
                    head_segments.emplace_back();
                } else {
                    throw BadRequest("unsupported media in the request");
                }
            }
        }

        // The reference truncates the state so everything else fits.
        int64_t fixed = int64_t(schema.size() + suffix.size());
        for (const auto& s : head_segments) fixed += s.size();
        if (fixed > opt_.n_ctx)
            throw BadRequest("schema requires " + std::to_string(fixed) + " tokens before state; maximum is " + std::to_string(opt_.n_ctx));
        std::vector<llama_token> state_tokens = tokenize(render(state), false);
        if (int64_t(state_tokens.size()) > opt_.n_ctx - fixed) state_tokens.resize(size_t(opt_.n_ctx - fixed));

        std::vector<Segment> segments = std::move(head_segments);
        {
            auto& tail = segments.back().tokens;
            tail.insert(tail.end(), state_tokens.begin(), state_tokens.end());
        }
        int64_t schema_offset = 0;
        for (const auto& s : segments) schema_offset += s.size();
        {
            auto& tail = segments.back().tokens;
            tail.insert(tail.end(), schema.begin(), schema.end());
            tail.insert(tail.end(), suffix.begin(), suffix.end());
        }
        int64_t total = 0;
        for (const auto& s : segments) total += s.size();

        // ---- backbone: last-layer hidden state of every position
        clef::Vec hidden = embed(segments, total);

        // ---- head
        std::vector<clef::QuestionSpans> spans(questions.size());
        std::vector<clef::Vec> lexical(questions.size());
        for (size_t qi = 0; qi < questions.size(); ++qi) {
            spans[qi].type = questions[qi].type_index;
            spans[qi].question_start = rel[qi].q0 + schema_offset;
            spans[qi].question_end = rel[qi].q1 + schema_offset;
            lexical[qi].assign(rel[qi].options.size() * size_t(n_embd_), 0.f);
            for (size_t oi = 0; oi < rel[qi].options.size(); ++oi) {
                const auto [s, e] = rel[qi].options[oi];
                spans[qi].options.push_back({s + schema_offset, e + schema_offset});
                float* dst = lexical[qi].data() + oi * size_t(n_embd_);
                for (int64_t t = s; t < e; ++t) lexical_->add_row(schema[size_t(t)], dst);
                const float inv = 1.0f / float(e - s);
                for (int64_t j = 0; j < n_embd_; ++j) dst[j] *= inv;
            }
        }
        std::vector<clef::Vec> logits = head_->forward(hidden, total, spans, lexical);

        ojson answers = ojson::object();
        for (size_t qi = 0; qi < questions.size(); ++qi) {
            const Question& q = questions[qi];
            std::vector<double> p = softmax(logits[qi]);
            auto prob_of = [&](const std::string& id) {
                for (size_t i = 0; i < q.options.size(); ++i)
                    if (q.options[i].id == id) return p[i];
                return 0.0;
            };
            ojson a = ojson::object();
            a["type"] = q.type;
            if (q.type == "noul") {
                const double yes = prob_of("true");
                a["noul"] = r4(yes);
                a["confidence"] = r4(std::max(yes, 1.0 - yes));
            } else if (q.type == "choice") {
                std::string best = q.request[0];
                for (const auto& id : q.request)
                    if (prob_of(id) > prob_of(best)) best = id;
                ojson dist = ojson::object();
                for (const auto& id : q.request) dist[id] = r4(prob_of(id));
                a["choice"] = best;
                a["probabilities"] = dist;
                a["confidence"] = r4(prob_of(best));
            } else {
                double score = 0, top = 0;
                ojson dist = ojson::object();
                for (size_t i = 0; i < p.size(); ++i) {
                    score += double(i) * p[i];
                    top = std::max(top, p[i]);
                    dist[std::to_string(i)] = r4(p[i]);
                }
                a["score"] = r4(score);
                a["probabilities"] = dist;
                a["confidence"] = r4(top);
            }
            answers[q.id] = a;
        }
        const double ms = std::chrono::duration<double, std::milli>(std::chrono::steady_clock::now() - t0).count();
        return ojson{{"answers", answers},
                     {"usage",
                      {{"input_tokens", total},
                       {"output_tokens", 0},
                       {"images", int(images.size())},
                       {"latency_ms", std::round(ms * 1000.0) / 1000.0}}}};
    }

private:
    static std::vector<double> softmax(const clef::Vec& z) {
        std::vector<double> p(z.size());
        double m = -INFINITY;
        for (float v : z) m = std::max(m, double(v));
        double sum = 0;
        for (size_t i = 0; i < z.size(); ++i) sum += (p[i] = std::exp(double(z[i]) - m));
        for (auto& v : p) v = (std::isfinite(sum) && sum > 0) ? v / sum : 1.0 / double(z.size());
        return p;
    }

    std::vector<llama_token> tokenize(const std::string& s, bool special) const {
        if (s.empty()) return {};
        int32_t r = llama_tokenize(vocab_, s.data(), int32_t(s.size()), nullptr, 0, false, special);
        int32_t n = r < 0 ? -r : r;
        std::vector<llama_token> out(size_t(std::max(n, 0)));
        if (n > 0) {
            int32_t got = llama_tokenize(vocab_, s.data(), int32_t(s.size()), out.data(), n, false, special);
            if (got < 0) throw std::runtime_error("tokenization failed");
            out.resize(size_t(got));
        }
        return out;
    }

    // Hidden state of every input position, [total, n_embd].
    clef::Vec embed(const std::vector<Segment>& segments, int64_t total) {
        llama_memory_clear(llama_get_memory(ctx_), true);
        clef::Vec hidden(size_t(total) * size_t(n_embd_));
        llama_pos n_past = 0;
        int64_t row = 0;
        for (const auto& seg : segments) {
            if (seg.image) {
                const int64_t n = seg.size();
                if (mtmd_encode_chunk(mtmd_, seg.image) != 0) throw std::runtime_error("image encoding failed");
                Capture cap{ctx_, hidden.data() + size_t(row) * size_t(n_embd_), n_embd_, n, 0};
                llama_pos next = 0;
                if (mtmd_helper_decode_image_chunk(mtmd_, ctx_, seg.image, mtmd_get_output_embd(mtmd_), n_past, 0, kBatch, &next,
                                                   capture_cb, &cap) != 0 ||
                    cap.got != n)
                    throw std::runtime_error("image decode failed");
                n_past = next;
                row += n;
                continue;
            }
            for (size_t off = 0; off < seg.tokens.size(); off += kBatch) {
                const size_t n = std::min<size_t>(kBatch, seg.tokens.size() - off);
                for (size_t i = 0; i < n; ++i) {
                    batch_.token[i] = seg.tokens[off + i];
                    batch_.pos[i] = n_past + llama_pos(i);
                    batch_.n_seq_id[i] = 1;
                    batch_.seq_id[i][0] = 0;
                    batch_.logits[i] = 1;
                }
                batch_.n_tokens = int32_t(n);
                int rc = llama_decode(ctx_, batch_);
                if (rc != 0) throw std::runtime_error("llama_decode returned " + std::to_string(rc));
                const float* e = llama_get_embeddings(ctx_);
                if (!e) throw std::runtime_error("backbone returned no hidden states");
                std::memcpy(hidden.data() + size_t(row) * size_t(n_embd_), e, n * size_t(n_embd_) * sizeof(float));
                n_past += llama_pos(n);
                row += int64_t(n);
            }
        }
        if (row != total) throw std::runtime_error("backbone produced an unexpected number of positions");
        return hidden;
    }

    static void load_backends() {
        // Backends (CUDA, CPU variants) are dynamic modules next to the
        // engine (lib/) in the release bundle.
        char exe[4096];
        ssize_t n = readlink("/proc/self/exe", exe, sizeof(exe) - 1);
        if (n > 0) {
            exe[n] = 0;
            std::string dir(exe);
            dir = dir.substr(0, dir.find_last_of('/'));
            ggml_backend_load_all_from_path((dir + "/lib").c_str());
            ggml_backend_load_all_from_path(dir.c_str());
        }
        ggml_backend_load_all();
    }

    Options opt_;
    int threads_ = 1;
    llama_model* model_ = nullptr;
    llama_context* ctx_ = nullptr;
    const llama_vocab* vocab_ = nullptr;
    mtmd_context* mtmd_ = nullptr;
    int64_t n_embd_ = 0;
    llama_batch batch_{};
    bool batch_ok_ = false;
    std::unique_ptr<clef::Head> head_;
    std::unique_ptr<OutputEmbeddings> lexical_;
};

void usage() {
    fprintf(stderr,
            "usage: clef --model <model.gguf> --head <joint_head.safetensors> [--mmproj <mmproj.gguf>]\n"
            "            [--device auto|cpu|cuda|cuda:N|vulkan|vulkan:N]\n"
            "            [--ctx N] [--threads N] [--gpu-layers N] [--flash-attn auto|on|off]\n"
            "            [--image-min-tokens N] [--image-max-tokens N] [--verbose]\n"
            "Speaks SELFIPC1 frames on stdin/stdout.\n");
}

} // namespace

int main(int argc, char** argv) {
    Options o;
    for (int i = 1; i < argc; ++i) {
        std::string a = argv[i];
        auto need = [&](const char* flag) -> std::string {
            if (i + 1 >= argc) {
                fprintf(stderr, "missing value for %s\n", flag);
                exit(2);
            }
            return argv[++i];
        };
        if (a == "--model") o.model = need("--model");
        else if (a == "--head") o.head = need("--head");
        else if (a == "--mmproj") o.mmproj = need("--mmproj");
        else if (a == "--device") o.device = need("--device");
        else if (a == "--ctx") o.n_ctx = std::atoi(need("--ctx").c_str());
        else if (a == "--threads") o.threads = std::atoi(need("--threads").c_str());
        else if (a == "--gpu-layers") o.gpu_layers = std::atoi(need("--gpu-layers").c_str());
        else if (a == "--flash-attn") o.flash_attn = need("--flash-attn");
        else if (a == "--image-min-tokens") o.image_min_tokens = std::atoi(need("--image-min-tokens").c_str());
        else if (a == "--image-max-tokens") o.image_max_tokens = std::atoi(need("--image-max-tokens").c_str());
        else if (a == "--verbose") g_verbose = true;
        else if (a == "-h" || a == "--help") {
            usage();
            return 0;
        } else {
            fprintf(stderr, "unknown argument: %s\n", a.c_str());
            usage();
            return 2;
        }
    }
    if (o.model.empty() || o.head.empty() || o.n_ctx < 512 ||
        (o.flash_attn != "auto" && o.flash_attn != "on" && o.flash_attn != "off")) {
        usage();
        return 2;
    }

    // Anything a library prints to stdout would corrupt the protocol: keep a
    // private handle to the real stdout and point fd 1 at stderr.
    int proto_fd = dup(STDOUT_FILENO);
    dup2(STDERR_FILENO, STDOUT_FILENO);
    if (!freopen(nullptr, "wb", stdout)) {}
    FILE* proto = fdopen(proto_fd, "wb");
    if (!proto) {
        perror("fdopen");
        return 1;
    }
    stdout = proto;

    Engine engine(o);
    try {
        engine.load();
    } catch (const std::exception& e) {
        logf("load failed: %s", e.what());
        return 1;
    }
    write_frame(engine.ready());
    logf("ready: %s%s", o.model.c_str(), o.mmproj.empty() ? "" : " (+vision)");

    for (;;) {
        Frame f;
        int r;
        try {
            r = read_frame(f);
        } catch (const std::exception& e) {
            logf("protocol error: %s", e.what());
            return 3;
        }
        if (r == 0) break; // stdin closed: clean shutdown

        uint64_t id = 0;
        ojson req;
        try {
            req = ojson::parse(f.header);
            id = req.value("id", uint64_t(0));
        } catch (const std::exception& e) {
            logf("malformed request header: %s", e.what());
            return 3;
        }
        if (id == 0) {
            logf("request without id");
            return 3;
        }
        try {
            std::string method = req.value("method", "");
            if (method != "systemone") throw BadRequest("unknown method '" + method + "'");
            ojson result = engine.systemone(req.contains("params") ? req["params"] : ojson::object(), f.attachments);
            write_frame(ojson{{"id", id}, {"result", result}});
        } catch (const BadRequest& e) {
            write_frame(ojson{{"id", id}, {"error", {{"type", "invalid_request"}, {"message", e.what()}}}});
        } catch (const std::exception& e) {
            logf("request %llu failed: %s", static_cast<unsigned long long>(id), e.what());
            write_frame(ojson{{"id", id}, {"error", {{"type", "internal_error"}, {"message", e.what()}}}});
        }
    }
    return 0;
}
