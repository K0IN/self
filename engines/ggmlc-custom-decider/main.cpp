// ggmlc-custom-decider: one-pass System One decision engine for Decider
// checkpoints (Mapika/decider, Qwen3.5 [+ vision]) stored as llama.cpp GGUFs.
//
// It reproduces the upstream readout (decider/prompt.py + decider/systemone.py
// + decider/vision/model.py): a lettered prompt, one forward pass, and a
// softmax over the option-letter logits at each "Answer: (" slot. Nothing is
// generated.
//
// Transport: SELFIPC1 frames on stdin/stdout (see internal/ipc in the Go
// server). stdout carries protocol frames only; every log goes to stderr.
//
//   frame := "SELFIPC1" | be32 header_len | header JSON | be32 n | (be64 len | bytes)*n
//
// Handshake (engine -> server, first frame):
//   {"type":"ready","protocol":1,"model":...,"device":...,
//    "capabilities":{"choice":true,"score":true,"noul":true,"max_options":10,
//                    "vision":{"enabled":true,"max_images":1,"input":{...}}}}
// Request:  {"id":N,"method":"systemone","params":{"state":<json>,
//            "questions":[{"id","type","instructions","criteria"}...],
//            "images":[{"attachment":0,"width":W,"height":H,"format":"rgb8"}]}}
// Response: {"id":N,"result":{"answers":{...},"usage":{...}}}
//        or {"id":N,"error":{"type":"invalid_request","message":"..."}}

#include "llama.h"
#include "ggml-backend.h"
#include "mtmd.h"
#include "mtmd-helper.h"

#include <nlohmann/json.hpp>

#include <algorithm>
#include <chrono>
#include <cmath>
#include <cstdarg>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <memory>
#include <regex>
#include <stdexcept>
#include <string>
#include <unistd.h>
#include <vector>

using json = nlohmann::ordered_json;

namespace {

// ---------------------------------------------------------------- logging

bool g_verbose = false;

void log_cb(enum ggml_log_level level, const char* text, void*) {
    if (g_verbose || level >= GGML_LOG_LEVEL_WARN) {
        fputs(text, stderr);
    }
}

void logf(const char* fmt, ...) __attribute__((format(printf, 1, 2)));
void logf(const char* fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    fputs("[decider] ", stderr);
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

void write_frame(const json& header) {
    std::string h = header.dump(-1, ' ', false, json::error_handler_t::replace);
    std::string out(kMagic, 8);
    put32(out, uint32_t(h.size()));
    out += h;
    put32(out, 0);
    fwrite(out.data(), 1, out.size(), stdout);
    fflush(stdout);
}

// ------------------------------------------------- System One rendering
// Mirrors decider/systemone.py (render_state, render_question, isolated
// score levels) and decider/prompt.py (plain layout, narrow options).

const std::string kLetters = "ABCDEFGHIJ";
constexpr int kNarrow = 10;
constexpr int kAnnotateMin = 8;

// json.dumps(x, ensure_ascii=False) formatting: ", " and ": " separators.
void py_dump(const json& j, std::string& o) {
    if (j.is_object()) {
        o += '{';
        bool first = true;
        for (auto it = j.begin(); it != j.end(); ++it) {
            if (!first) o += ", ";
            first = false;
            o += json(it.key()).dump(-1, ' ', false, json::error_handler_t::replace);
            o += ": ";
            py_dump(it.value(), o);
        }
        o += '}';
    } else if (j.is_array()) {
        o += '[';
        for (size_t i = 0; i < j.size(); ++i) {
            if (i) o += ", ";
            py_dump(j[i], o);
        }
        o += ']';
    } else {
        o += j.dump(-1, ' ', false, json::error_handler_t::replace);
    }
}

std::string py_dumps(const json& j) {
    std::string o;
    py_dump(j, o);
    return o;
}

// Long arrays get their element positions written in ({"_index": i, ...}).
json annotate_indices(const json& x) {
    if (x.is_array()) {
        json out = json::array();
        bool annotate = x.size() >= size_t(kAnnotateMin);
        for (size_t i = 0; i < x.size(); ++i) {
            json v = annotate_indices(x[i]);
            if (!annotate) {
                out.push_back(v);
            } else if (v.is_object()) {
                json o = json::object();
                o["_index"] = i;
                for (auto it = v.begin(); it != v.end(); ++it) o[it.key()] = it.value();
                out.push_back(o);
            } else {
                out.push_back(json{{"_index", i}, {"value", v}});
            }
        }
        return out;
    }
    if (x.is_object()) {
        json out = json::object();
        for (auto it = x.begin(); it != x.end(); ++it) out[it.key()] = annotate_indices(it.value());
        return out;
    }
    return x;
}

std::string render_state(const json& state) {
    if (state.is_string()) return state.get<std::string>();
    if (state.is_null()) return "";
    return py_dumps(annotate_indices(state));
}

std::string txt(const json& v) {
    return v.is_string() ? v.get<std::string>() : py_dumps(v);
}

std::string strip_level_number(const std::string& s) {
    static const std::regex re(R"(^\s*-?\d+\s*:\s*)");
    return std::regex_replace(s, re, "", std::regex_constants::format_first_only);
}

struct Row {
    std::string question;
    std::vector<std::string> options;
};

struct Planned {
    std::string id;
    std::string type;               // choice | score | noul
    std::vector<std::string> names; // choice: option names
    int first_row = 0;
    int n_rows = 1;
    bool isolated = false; // score read with one yes/no row per level
};

struct BadRequest : std::runtime_error {
    using std::runtime_error::runtime_error;
};

void plan(const json& questions, std::vector<Row>& rows, std::vector<Planned>& plan) {
    if (!questions.is_array() || questions.empty()) throw BadRequest("questions must be a non-empty array");
    for (const auto& q : questions) {
        Planned p;
        p.id = q.value("id", "");
        p.type = q.value("type", "");
        if (p.id.empty()) throw BadRequest("question without id");
        const json ins = q.contains("instructions") ? q["instructions"] : json();
        std::string instructions = ins.is_null() ? "" : txt(ins);
        if (instructions.empty()) throw BadRequest("question " + p.id + ": instructions are required");
        const json crit = q.contains("criteria") ? q["criteria"] : json();
        p.first_row = int(rows.size());
        if (p.type == "choice") {
            Row r{instructions, {}};
            if (crit.is_object()) {
                for (auto it = crit.begin(); it != crit.end(); ++it) {
                    p.names.push_back(it.key());
                    const json& d = it.value();
                    bool empty = d.is_null() || (d.is_string() && d.get<std::string>().empty());
                    r.options.push_back(empty ? it.key() : it.key() + ": " + txt(d));
                }
            } else if (crit.is_array()) {
                for (const auto& n : crit) {
                    p.names.push_back(txt(n));
                    r.options.push_back(txt(n));
                }
            }
            if (r.options.empty()) throw BadRequest("question " + p.id + ": choice criteria are required");
            if (int(r.options.size()) > kNarrow)
                throw BadRequest("question " + p.id + ": at most " + std::to_string(kNarrow) + " options are supported");
            rows.push_back(std::move(r));
        } else if (p.type == "score") {
            if (!crit.is_array() || crit.size() < 2) throw BadRequest("question " + p.id + ": score criteria need at least 2 levels");
            if (crit.size() > size_t(kNarrow)) throw BadRequest("question " + p.id + ": at most 10 score levels are supported");
            p.isolated = true;
            for (const auto& level : crit) {
                rows.push_back(Row{instructions + "\nProposed answer: " + strip_level_number(txt(level)) +
                                       "\nDoes the proposed answer fit?",
                                   {"no", "yes"}});
            }
            p.n_rows = int(crit.size());
        } else if (p.type == "noul") {
            rows.push_back(Row{instructions, {"no", "yes"}});
        } else {
            throw BadRequest("question " + p.id + ": unknown type '" + p.type + "'");
        }
        plan.push_back(std::move(p));
    }
}

// Plain state-first layout (decider/prompt.py build()).
std::string render_prompt(const std::string& state_text, const std::vector<Row>& rows) {
    std::string s = "Context:\n" + state_text;
    bool multi = rows.size() > 1;
    for (size_t k = 0; k < rows.size(); ++k) {
        std::string num = multi ? " " + std::to_string(k + 1) : "";
        s += "\n\nQuestion" + num + ": " + rows[k].question + "\nOptions:";
        for (size_t j = 0; j < rows[k].options.size(); ++j) {
            s += "\n(";
            s += kLetters[j];
            s += ") " + rows[k].options[j];
        }
        s += "\nAnswer" + num + ": (";
    }
    return s;
}

double r4(double x) {
    return std::round(x * 10000.0) / 10000.0;
}

double clip01(double x) {
    return std::min(1.0, std::max(0.0, x));
}

double choice_confidence(const std::vector<double>& p) {
    size_t n = p.size();
    if (n <= 1) return 1.0;
    double m = *std::max_element(p.begin(), p.end());
    return clip01((double(n) * m - 1.0) / double(n - 1));
}

double score_confidence(const std::vector<double>& p) {
    size_t n = p.size();
    if (n <= 1) return 1.0;
    size_t k = size_t(std::max_element(p.begin(), p.end()) - p.begin());
    double spread = 0, uniform = 0;
    for (size_t i = 0; i < n; ++i) {
        spread += p[i] * std::fabs(double(i) - double(k));
        uniform += std::fabs(double(i) - double(n - 1) / 2.0);
    }
    uniform /= double(n);
    return clip01(1.0 - spread / uniform);
}

// ---------------------------------------------------------------- engine

struct Options {
    std::string model, mmproj, device = "auto";
    int n_ctx = 8192;
    int threads = 0;
    float temperature = 1.0f;
    int gpu_layers = -1;           // -1 = all
    std::string flash_attn = "auto"; // auto | on | off
    int image_min_tokens = 0;      // 0 = from mmproj metadata
    int image_max_tokens = 0;
};

class Engine {
public:
    explicit Engine(const Options& o) : opt_(o) {}

    ~Engine() {
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

        bool cpu = opt_.device == "cpu";
        llama_model_params mp = llama_model_default_params();
        mp.n_gpu_layers = cpu ? 0 : opt_.gpu_layers;
        if (opt_.device.rfind("cuda:", 0) == 0 || opt_.device.rfind("vulkan:", 0) == 0) {
            mp.main_gpu = std::atoi(opt_.device.substr(opt_.device.find(':') + 1).c_str());
            mp.split_mode = LLAMA_SPLIT_MODE_NONE;
        }
        model_ = llama_model_load_from_file(opt_.model.c_str(), mp);
        if (!model_) throw std::runtime_error("cannot load model " + opt_.model);

        llama_context_params cp = llama_context_default_params();
        // llama.cpp aborts when one decode exceeds n_batch; one row is one decode.
        cp.n_ctx = uint32_t(opt_.n_ctx);
        cp.n_batch = uint32_t(opt_.n_ctx);
        cp.n_ubatch = uint32_t(std::min(2048, opt_.n_ctx));
        cp.n_seq_max = 1;
        cp.flash_attn_type = flash_type();
        if (opt_.threads > 0) cp.n_threads = cp.n_threads_batch = opt_.threads;
        ctx_ = llama_init_from_model(model_, cp);
        if (!ctx_) throw std::runtime_error("cannot create llama context");
        vocab_ = llama_model_get_vocab(model_);
        n_vocab_ = llama_vocab_n_tokens(vocab_);

        if (!opt_.mmproj.empty()) {
            mtmd_context_params mcp = mtmd_context_params_default();
            mcp.use_gpu = !cpu;
            mcp.print_timings = false;
            mcp.warmup = false;
            if (opt_.threads > 0) mcp.n_threads = opt_.threads;
            mcp.flash_attn_type = flash_type();
            if (opt_.image_min_tokens > 0) mcp.image_min_tokens = opt_.image_min_tokens;
            if (opt_.image_max_tokens > 0) mcp.image_max_tokens = opt_.image_max_tokens;
            mtmd_ = mtmd_init_from_file(opt_.mmproj.c_str(), model_, mcp);
            if (!mtmd_) throw std::runtime_error("cannot load mmproj " + opt_.mmproj);
            if (!mtmd_support_vision(mtmd_)) throw std::runtime_error("mmproj has no vision encoder");
        }

        // Option-letter tokens and the answer-slot pattern ":" + " (".
        for (char c : kLetters) {
            auto t = tokenize(std::string(1, c));
            if (t.size() != 1) throw std::runtime_error(std::string("letter ") + c + " is not a single token");
            letters_.push_back(t[0]);
        }
        auto ans = tokenize("Answer: (");
        auto colon = tokenize(":");
        if (ans.size() < 2 || colon.size() != 1 || ans[ans.size() - 2] != colon[0])
            throw std::runtime_error("tokenizer does not split 'Answer: (' into ':' and ' ('");
        colon_tok_ = colon[0];
        slot_tok_ = ans.back();
    }

    json ready() const {
        char name[256] = {0};
        llama_model_meta_val_str(model_, "general.name", name, sizeof(name));
        json vision = {{"enabled", mtmd_ != nullptr}};
        if (mtmd_) {
            vision["max_images"] = 1;
            // mtmd does the exact Qwen-VL smart resize; the server only needs
            // to bound the size so no oversized pixels travel over the pipe.
            vision["input"] = {{"mode", "bounded"}, {"max_width", 1536}, {"max_height", 1536}, {"resize", "contain"}};
        }
        return json{{"type", "ready"},
                    {"protocol", 1},
                    {"model", name},
                    {"device", opt_.device},
                    {"capabilities",
                     {{"text", true},
                      {"choice", true},
                      {"score", true},
                      {"noul", true},
                      {"max_options", kNarrow},
                      {"vision", vision}}}};
    }

    json systemone(const json& params, const std::vector<std::vector<uint8_t>>& attachments) {
        auto t0 = std::chrono::steady_clock::now();
        std::vector<Row> rows;
        std::vector<Planned> planned;
        plan(params.contains("questions") ? params["questions"] : json(), rows, planned);

        std::string state_text = render_state(params.contains("state") ? params["state"] : json());
        std::string prompt = render_prompt(state_text, rows);

        const json images = params.contains("images") ? params["images"] : json::array();
        if (images.size() > 1) throw BadRequest("at most one image per request is supported");
        if (!images.empty() && !mtmd_) throw BadRequest("this model has no vision encoder");

        llama_memory_clear(llama_get_memory(ctx_), true);

        std::vector<llama_token> tail; // tokens of the final text chunk
        llama_pos n_past = 0;
        int n_input = 0;
        if (!images.empty()) {
            const json& im = images[0];
            size_t ai = im.value("attachment", size_t(0));
            int w = im.value("width", 0), h = im.value("height", 0);
            if (im.value("format", "") != "rgb8") throw BadRequest("image format must be rgb8");
            if (ai >= attachments.size() || w <= 0 || h <= 0 ||
                attachments[ai].size() != size_t(w) * size_t(h) * 3)
                throw BadRequest("image attachment does not match its width/height");
            std::unique_ptr<mtmd_bitmap, decltype(&mtmd_bitmap_free)> bmp(
                mtmd_bitmap_init(uint32_t(w), uint32_t(h), attachments[ai].data()), mtmd_bitmap_free);
            std::string text = std::string(mtmd_default_marker()) + prompt;
            mtmd_input_text in{text.c_str(), text.size(), false, false};
            std::unique_ptr<mtmd_input_chunks, decltype(&mtmd_input_chunks_free)> chunks(mtmd_input_chunks_init(),
                                                                                         mtmd_input_chunks_free);
            const mtmd_bitmap* bitmaps[1] = {bmp.get()};
            if (mtmd_tokenize(mtmd_, chunks.get(), &in, bitmaps, 1) != 0) throw std::runtime_error("mtmd_tokenize failed");
            size_t n = mtmd_input_chunks_size(chunks.get());
            if (n == 0) throw std::runtime_error("empty prompt");
            const mtmd_input_chunk* last = mtmd_input_chunks_get(chunks.get(), n - 1);
            if (mtmd_input_chunk_get_type(last) != MTMD_INPUT_CHUNK_TYPE_TEXT) throw std::runtime_error("prompt must end with text");
            for (size_t i = 0; i + 1 < n; ++i) {
                const mtmd_input_chunk* c = mtmd_input_chunks_get(chunks.get(), i);
                n_input += int(mtmd_input_chunk_get_n_tokens(c));
                llama_pos np = 0;
                if (mtmd_helper_eval_chunk_single(mtmd_, ctx_, c, n_past, 0, opt_.n_ctx, false, &np) != 0)
                    throw std::runtime_error("image/prefix decode failed");
                n_past = np;
            }
            size_t nt = 0;
            const llama_token* toks = mtmd_input_chunk_get_tokens_text(last, &nt);
            tail.assign(toks, toks + nt);
        } else {
            tail = tokenize(prompt);
        }
        n_input += int(tail.size());
        if (n_past + llama_pos(tail.size()) >= opt_.n_ctx)
            throw BadRequest("prompt is too long (" + std::to_string(n_past + tail.size()) + " tokens, limit " +
                             std::to_string(opt_.n_ctx) + ")");

        // Answer slots: ":" followed by " (", the last len(rows) occurrences
        // (a state may itself quote "Answer 1: (").
        std::vector<int> slots;
        for (size_t i = 1; i < tail.size(); ++i)
            if (tail[i] == slot_tok_ && tail[i - 1] == colon_tok_) slots.push_back(int(i));
        if (slots.size() < rows.size()) throw std::runtime_error("answer slots not found in the tokenized prompt");
        slots.erase(slots.begin(), slots.end() - long(rows.size()));

        llama_batch batch = llama_batch_init(int32_t(tail.size()), 0, 1);
        std::vector<char> want(tail.size(), 0);
        for (int s : slots) want[size_t(s)] = 1;
        for (size_t i = 0; i < tail.size(); ++i) {
            batch.token[i] = tail[i];
            batch.pos[i] = n_past + llama_pos(i);
            batch.n_seq_id[i] = 1;
            batch.seq_id[i][0] = 0;
            batch.logits[i] = want[i];
        }
        batch.n_tokens = int32_t(tail.size());
        int rc = llama_decode(ctx_, batch);
        if (rc != 0) {
            llama_batch_free(batch);
            throw std::runtime_error("llama_decode returned " + std::to_string(rc));
        }

        // Letter logits -> probabilities per row.
        std::vector<std::vector<double>> probs(rows.size());
        for (size_t r = 0; r < rows.size(); ++r) {
            const float* lg = llama_get_logits_ith(ctx_, slots[r]);
            size_t k = rows[r].options.size();
            std::vector<double> z(k);
            double m = -INFINITY;
            for (size_t j = 0; j < k; ++j) {
                z[j] = double(lg[letters_[j]]) / double(opt_.temperature);
                m = std::max(m, z[j]);
            }
            double sum = 0;
            for (auto& v : z) sum += (v = std::exp(v - m));
            for (auto& v : z) v = (std::isfinite(sum) && sum > 0) ? v / sum : 1.0 / double(k);
            probs[r] = std::move(z);
        }
        llama_batch_free(batch);

        json answers = json::object();
        for (const auto& p : planned) {
            json a = json::object();
            a["type"] = p.type;
            if (p.type == "choice") {
                const auto& pr = probs[size_t(p.first_row)];
                size_t j = size_t(std::max_element(pr.begin(), pr.end()) - pr.begin());
                json dist = json::object();
                for (size_t i = 0; i < pr.size(); ++i) dist[p.names[i]] = r4(pr[i]);
                a["choice"] = p.names[j];
                a["probabilities"] = dist;
                a["confidence"] = r4(choice_confidence(pr));
            } else if (p.type == "noul") {
                double yes = probs[size_t(p.first_row)][1];
                a["noul"] = r4(yes);
                a["confidence"] = r4(choice_confidence({1.0 - yes, yes}));
            } else { // isolated score: P(level fits) per level, normalised
                std::vector<double> fit;
                for (int i = 0; i < p.n_rows; ++i) fit.push_back(probs[size_t(p.first_row + i)][1]);
                double tot = 0;
                for (double f : fit) tot += f;
                if (tot <= 0) tot = 1e-9;
                std::vector<double> dist(fit.size());
                double score = 0;
                json dj = json::object();
                for (size_t i = 0; i < fit.size(); ++i) {
                    dist[i] = fit[i] / tot;
                    score += double(i) * dist[i];
                    dj[std::to_string(i)] = r4(dist[i]);
                }
                a["score"] = r4(score);
                a["probabilities"] = dj;
                a["confidence"] = r4(score_confidence(dist));
            }
            answers[p.id] = a;
        }
        double ms = std::chrono::duration<double, std::milli>(std::chrono::steady_clock::now() - t0).count();
        return json{{"answers", answers},
                    {"usage",
                     {{"input_tokens", n_input},
                      {"output_tokens", 0},
                      {"images", int(images.size())},
                      {"latency_ms", std::round(ms * 1000.0) / 1000.0}}}};
    }

private:
    std::vector<llama_token> tokenize(const std::string& s) const {
        int n = -llama_tokenize(vocab_, s.data(), int32_t(s.size()), nullptr, 0, false, false);
        std::vector<llama_token> out(size_t(std::max(n, 0)));
        if (n > 0) llama_tokenize(vocab_, s.data(), int32_t(s.size()), out.data(), n, false, false);
        return out;
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
    llama_model* model_ = nullptr;
    llama_context* ctx_ = nullptr;
    const llama_vocab* vocab_ = nullptr;
    int32_t n_vocab_ = 0;
    mtmd_context* mtmd_ = nullptr;
    std::vector<llama_token> letters_;
    llama_token colon_tok_ = -1, slot_tok_ = -1;
};

void usage() {
    fprintf(stderr,
            "usage: ggmlc-custom-decider --model <model.gguf> [--mmproj <mmproj.gguf>]\n"
            "                            [--device auto|cpu|cuda|cuda:N|vulkan|vulkan:N]\n"
            "                            [--ctx N] [--threads N] [--temperature T] [--gpu-layers N]\n"
            "                            [--flash-attn auto|on|off] [--image-min-tokens N]\n"
            "                            [--image-max-tokens N] [--verbose]\n"
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
        else if (a == "--mmproj") o.mmproj = need("--mmproj");
        else if (a == "--device") o.device = need("--device");
        else if (a == "--ctx") o.n_ctx = std::atoi(need("--ctx").c_str());
        else if (a == "--threads") o.threads = std::atoi(need("--threads").c_str());
        else if (a == "--temperature") o.temperature = std::strtof(need("--temperature").c_str(), nullptr);
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
    if (o.model.empty() || o.n_ctx < 512 || !(o.temperature > 0) ||
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
        json req;
        try {
            req = json::parse(f.header);
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
            json result = engine.systemone(req.contains("params") ? req["params"] : json::object(), f.attachments);
            write_frame(json{{"id", id}, {"result", result}});
        } catch (const BadRequest& e) {
            write_frame(json{{"id", id}, {"error", {{"type", "invalid_request"}, {"message", e.what()}}}});
        } catch (const std::exception& e) {
            write_frame(json{{"id", id}, {"error", {{"type", "internal_error"}, {"message", e.what()}}}});
        }
    }
    return 0;
}
