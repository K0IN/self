// ggmlc-audio: persistent text-to-speech engine for llama.cpp audio models
// (Qwen3-TTS, Pocket TTS) via libmtmd's audio-generation helper.
//
// The model, projector and generation context are loaded once at startup and
// reused for every request, unlike llama.cpp's one-shot `llama-tts` CLI. The
// generation loop mirrors tools/tts/tts.cpp of the pinned llama.cpp tag.
//
// Transport: SELFIPC1 frames on stdin/stdout (see internal/ipc in the Go
// server). stdout carries protocol frames only; every log goes to stderr.
//
// Handshake (engine -> server, first frame):
//   {"type":"ready","protocol":1,"model":...,"device":...,
//    "audio":{"pipeline":"qwen3tts","sample_rate":24000}}
// Request:  {"id":N,"method":"synthesize","params":{"input":"...","language":"en",
//            "speaker_attachment":0}}   (attachment 0 = MP3/WAV reference audio)
// Response: {"id":N,"result":{"sample_rate":24000,"frames":F,"samples":S,"latency_ms":T}}
//           + attachment 0 = the WAV file
//        or {"id":N,"error":{"type":"invalid_request","message":"..."}}

#include "llama.h"
#include "ggml-backend.h"
#include "mtmd.h"
#include "mtmd-helper.h"

#include <nlohmann/json.hpp>

#include <chrono>
#include <cmath>
#include <cstdarg>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <memory>
#include <stdexcept>
#include <string>
#include <thread>
#include <unistd.h>
#include <vector>

using json = nlohmann::ordered_json;

namespace {

bool g_verbose = false;

void log_cb(enum ggml_log_level level, const char* text, void*) {
    if (g_verbose || level >= GGML_LOG_LEVEL_WARN) fputs(text, stderr);
}

void logf(const char* fmt, ...) __attribute__((format(printf, 1, 2)));
void logf(const char* fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    fputs("[audio] ", stderr);
    vfprintf(stderr, fmt, ap);
    fputc('\n', stderr);
    va_end(ap);
}

// ---------------------------------------------------------------- framing

constexpr char kMagic[8] = {'S', 'E', 'L', 'F', 'I', 'P', 'C', '1'};
constexpr uint32_t kMaxHeader = 4u << 20;
constexpr uint32_t kMaxAttachments = 8;
constexpr uint64_t kMaxAttachment = 128ull << 20;

struct Frame {
    std::string header;
    std::vector<std::vector<uint8_t>> attachments;
};

bool read_exact(void* dst, size_t n) { return fread(dst, 1, n, stdin) == n; }

uint32_t be32(const uint8_t* b) {
    return (uint32_t(b[0]) << 24) | (uint32_t(b[1]) << 16) | (uint32_t(b[2]) << 8) | uint32_t(b[3]);
}

uint64_t be64(const uint8_t* b) { return (uint64_t(be32(b)) << 32) | be32(b + 4); }

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

void put64(std::string& o, uint64_t v) {
    for (int s = 56; s >= 0; s -= 8) o.push_back(char((v >> s) & 0xff));
}

void write_frame(const json& header, const char* attachment = nullptr, size_t len = 0) {
    std::string h = header.dump(-1, ' ', false, json::error_handler_t::replace);
    std::string out(kMagic, 8);
    put32(out, uint32_t(h.size()));
    out += h;
    put32(out, attachment ? 1 : 0);
    if (attachment) put64(out, len);
    fwrite(out.data(), 1, out.size(), stdout);
    if (attachment) fwrite(attachment, 1, len, stdout);
    fflush(stdout);
}

struct BadRequest : std::runtime_error {
    using std::runtime_error::runtime_error;
};

// ---------------------------------------------------------------- engine

struct Options {
    std::string model, mmproj, device = "auto";
    int n_ctx = 4096;
    int threads = 0;
    int gpu_layers = -1; // -1 = all
    float temperature = 0.8f;
    int top_k = 40;
    float top_p = 0.95f;
    int max_frames = 512;
    long long seed = -1; // -1 = random per request
};

class Engine {
public:
    explicit Engine(const Options& o) : opt_(o) {}

    ~Engine() {
        gen_.reset();
        if (mtmd_) mtmd_free(mtmd_);
        if (ctx_) llama_free(ctx_);
        if (model_) llama_model_free(model_);
        llama_backend_free();
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

        int threads = opt_.threads > 0 ? opt_.threads : std::max(1u, std::thread::hardware_concurrency() / 2);
        llama_context_params cp = llama_context_default_params();
        cp.n_ctx = uint32_t(opt_.n_ctx);
        cp.n_batch = uint32_t(std::min(2048, opt_.n_ctx));
        cp.n_seq_max = 1;
        cp.embeddings = true; // the generator needs the backbone hidden states
        cp.n_threads = cp.n_threads_batch = threads;
        ctx_ = llama_init_from_model(model_, cp);
        if (!ctx_) throw std::runtime_error("cannot create llama context");

        mtmd_context_params mcp = mtmd_context_params_default();
        mcp.use_gpu = !cpu;
        mcp.print_timings = false;
        mcp.n_threads = threads;
        mtmd_ = mtmd_init_from_file(opt_.mmproj.c_str(), model_, mcp);
        if (!mtmd_) throw std::runtime_error("cannot load mmproj " + opt_.mmproj);
        info_ = mtmd_gen_audio_get_info(mtmd_);
        if (info_.type == MTMD_GEN_AUDIO_TYPE_NONE) throw std::runtime_error("mmproj does not support audio generation");
    }

    json ready() const {
        char name[256] = {0};
        llama_model_meta_val_str(model_, "general.name", name, sizeof(name));
        const char* pipeline = info_.type == MTMD_GEN_AUDIO_TYPE_QWEN3TTS ? "qwen3tts" : "pockettts";
        return json{{"type", "ready"},
                    {"protocol", 1},
                    {"model", name},
                    {"device", opt_.device},
                    {"audio", {{"pipeline", pipeline}, {"sample_rate", info_.sample_rate}}}};
    }

    // Returns the result header; the WAV bytes are left in wav_/wav_len_.
    json synthesize(const json& params, const std::vector<std::vector<uint8_t>>& attachments) {
        auto t0 = std::chrono::steady_clock::now();
        std::string input = params.value("input", "");
        if (input.empty()) throw BadRequest("input is required");
        std::string lang = params.value("language", "");

        mtmd::bitmap_ptr speaker;
        int si = params.value("speaker_attachment", -1);
        if (si >= 0) {
            if (size_t(si) >= attachments.size() || attachments[si].empty()) throw BadRequest("speaker attachment missing");
            auto w = mtmd_helper_bitmap_init_from_buf(mtmd_, attachments[si].data(), attachments[si].size(), false,
                                                      mtmd_helper_init_opt_default());
            if (!w.bitmap) throw BadRequest("cannot decode the reference audio (MP3 or WAV expected)");
            speaker.reset(w.bitmap);
        }

        // Each request starts from a clean state: the loaded weights are reused, but
        // the KV cache, generator and sampler are fresh (they keep per-run RNG and
        // audio state that reset() does not clear).
        llama_memory_clear(llama_get_memory(ctx_), true);
        gen_ = std::make_unique<mtmd_helper::gen_audio>(ctx_, mtmd_);
        if (!gen_->ctx) throw std::runtime_error("cannot create the audio generator");
        std::unique_ptr<llama_sampler, decltype(&llama_sampler_free)> smpl(new_sampler(), llama_sampler_free);
        llama_sampler* smpl_ = smpl.get();

        mtmd_helper_gen_audio_inp inp{};
        inp.seq_id = 0;
        inp.prompt = input.c_str();
        inp.prompt_len = input.size();
        inp.speaker_ref = speaker.get();
        inp.lang = lang.empty() ? nullptr : lang.c_str();
        inp.top_k = opt_.top_k;
        inp.top_p = opt_.top_p;
        inp.seed = seed();
        inp.out_type = MTMD_HELPER_GEN_AUDIO_OUTTYPE_WAV;
        if (gen_->set_input(&inp) != 0) throw std::runtime_error("set_input failed");
        for (;;) {
            int32_t r = gen_->step_prompt(int32_t(std::min(2048, opt_.n_ctx)));
            if (r < 0) throw std::runtime_error("prompt processing failed (input too long for the context?)");
            if (r == 0) break;
        }

        int frames = 0;
        llama_token sampled = llama_sampler_sample(smpl_, ctx_, -1);
        const float* h_state = llama_get_embeddings_ith(ctx_, -1);
        bool stop = false;
        while (!stop && frames < opt_.max_frames) {
            const float* h_next = nullptr;
            if (gen_->step_gen(sampled, h_state, &h_next, &stop) != 0)
                throw std::runtime_error("generation failed at frame " + std::to_string(frames));
            if (!h_next) break;
            ++frames;
            h_state = h_next;
            sampled = llama_sampler_sample(smpl_, ctx_, -1);
        }

        int32_t sample_rate = 0;
        int64_t samples = 0;
        if (gen_->get_output(&sample_rate, &wav_, &wav_len_, &samples) != 0) throw std::runtime_error("get_output failed");
        double ms = std::chrono::duration<double, std::milli>(std::chrono::steady_clock::now() - t0).count();
        return json{{"sample_rate", sample_rate},
                    {"frames", frames},
                    {"samples", samples},
                    {"latency_ms", std::round(ms * 1000.0) / 1000.0}};
    }

    const char* wav() const { return wav_; }
    size_t wav_len() const { return wav_len_; }

private:
    uint32_t seed() const { return opt_.seed < 0 ? LLAMA_DEFAULT_SEED : uint32_t(opt_.seed); }

    llama_sampler* new_sampler() const {
        llama_sampler* s = llama_sampler_chain_init(llama_sampler_chain_default_params());
        llama_sampler_chain_add(s, llama_sampler_init_top_k(opt_.top_k));
        llama_sampler_chain_add(s, llama_sampler_init_top_p(opt_.top_p, 1));
        llama_sampler_chain_add(s, llama_sampler_init_min_p(0.05f, 1));
        llama_sampler_chain_add(s, llama_sampler_init_temp(opt_.temperature));
        llama_sampler_chain_add(s, llama_sampler_init_dist(seed()));
        return s;
    }

    static void load_backends() {
        // Backend modules (CPU variants, CUDA, Vulkan) sit in lib/ next to the engine.
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
    mtmd_context* mtmd_ = nullptr;
    mtmd_gen_audio_info info_{};
    std::unique_ptr<mtmd_helper::gen_audio> gen_; // last request's; owns the WAV bytes
    const char* wav_ = nullptr;
    size_t wav_len_ = 0;
};

void usage() {
    fprintf(stderr,
            "usage: ggmlc-audio --model <model.gguf> --mmproj <mmproj.gguf>\n"
            "                   [--device auto|cpu|cuda|cuda:N|vulkan|vulkan:N] [--ctx N] [--threads N]\n"
            "                   [--gpu-layers N] [--temperature T] [--top-k N] [--top-p P]\n"
            "                   [--max-frames N] [--seed N] [--verbose]\n"
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
        else if (a == "--gpu-layers") o.gpu_layers = std::atoi(need("--gpu-layers").c_str());
        else if (a == "--temperature") o.temperature = std::strtof(need("--temperature").c_str(), nullptr);
        else if (a == "--top-k") o.top_k = std::atoi(need("--top-k").c_str());
        else if (a == "--top-p") o.top_p = std::strtof(need("--top-p").c_str(), nullptr);
        else if (a == "--max-frames") o.max_frames = std::atoi(need("--max-frames").c_str());
        else if (a == "--seed") o.seed = std::atoll(need("--seed").c_str());
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
    if (o.model.empty() || o.mmproj.empty() || o.n_ctx < 512 || o.max_frames < 1 || !(o.temperature > 0)) {
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
    logf("ready: %s", o.model.c_str());

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
            if (method != "synthesize") throw BadRequest("unknown method '" + method + "'");
            json result = engine.synthesize(req.contains("params") ? req["params"] : json::object(), f.attachments);
            write_frame(json{{"id", id}, {"result", result}}, engine.wav(), engine.wav_len());
        } catch (const BadRequest& e) {
            write_frame(json{{"id", id}, {"error", {{"type", "invalid_request"}, {"message", e.what()}}}});
        } catch (const std::exception& e) {
            write_frame(json{{"id", id}, {"error", {{"type", "internal_error"}, {"message", e.what()}}}});
        }
    }
    return 0;
}
