// Clef joint schema head, ported from Cloudflare's reference implementation
// (joint_schema_model.py: JointSchemaHead). Plain C++, float32, CPU only.
//
// Weights come from joint_head.safetensors (BF16 on disk). The head is small
// (about 120M parameters) next to the 9B backbone, so it runs on the CPU
// regardless of where the backbone runs.

#pragma once

#include <nlohmann/json.hpp>

#include <algorithm>
#include <cmath>
#include <cstdint>
#include <cstring>
#include <fcntl.h>
#include <stdexcept>
#include <string>
#include <sys/mman.h>
#include <sys/stat.h>
#include <thread>
#include <unistd.h>
#include <unordered_map>
#include <utility>
#include <vector>

namespace clef {

namespace gemm_generic {
#include "gemm_impl.hpp"
}

#if defined(__x86_64__) && defined(__GNUC__) && !defined(__clang__)
#define CLEF_HAVE_AVX2_PATH 1
#pragma GCC push_options
#pragma GCC target("avx2,fma")
namespace gemm_avx2 {
#include "gemm_impl.hpp"
}
#pragma GCC pop_options
#endif

using Vec = std::vector<float>;

struct Mat {
    int64_t r = 0, c = 0;
    Vec d;
    Mat() = default;
    Mat(int64_t rows, int64_t cols) : r(rows), c(cols), d(size_t(rows * cols)) {}
    float* row(int64_t i) { return d.data() + i * c; }
    const float* row(int64_t i) const { return d.data() + i * c; }
};

// ------------------------------------------------------------ threading

template <class F>
void parallel_for(int64_t n, int threads, F&& f) {
    int t = int(std::min<int64_t>(threads, n));
    if (t <= 1) {
        if (n > 0) f(int64_t(0), n);
        return;
    }
    int64_t chunk = (n + t - 1) / t;
    std::vector<std::thread> pool;
    pool.reserve(size_t(t - 1));
    for (int i = 1; i < t; ++i) {
        int64_t b = i * chunk, e = std::min(n, b + chunk);
        if (b >= e) break;
        pool.emplace_back([&f, b, e] { f(b, e); });
    }
    f(int64_t(0), std::min(n, chunk));
    for (auto& th : pool) th.join();
}

// y[M,N] = x[M,K] * W^T (+ b); W is [N,K] (a row per output feature).
inline void linear(const float* x, int64_t M, int64_t K, const float* W, int64_t N, const float* b, float* y, int threads) {
    if (double(M) * double(N) * double(K) < 2e6) threads = 1;
#ifdef CLEF_HAVE_AVX2_PATH
    static const bool avx2 = __builtin_cpu_supports("avx2") && __builtin_cpu_supports("fma");
#else
    const bool avx2 = false;
#endif
    parallel_for(N, threads, [&](int64_t n0, int64_t n1) {
#ifdef CLEF_HAVE_AVX2_PATH
        if (avx2) {
            gemm_avx2::gemm(x, M, K, W, n0, n1, y, N);
        } else
#endif
        {
            (void)avx2;
            gemm_generic::gemm(x, M, K, W, n0, n1, y, N);
        }
        if (b)
            for (int64_t m = 0; m < M; ++m)
                for (int64_t n = n0; n < n1; ++n) y[m * N + n] += b[n];
    });
}

inline void layer_norm(const float* x, int64_t rows, int64_t d, const Vec& g, const Vec& b, float* y, float eps = 1e-5f) {
    for (int64_t i = 0; i < rows; ++i) {
        const float* xi = x + i * d;
        double mean = 0;
        for (int64_t j = 0; j < d; ++j) mean += xi[j];
        mean /= double(d);
        double var = 0;
        for (int64_t j = 0; j < d; ++j) {
            double t = xi[j] - mean;
            var += t * t;
        }
        var /= double(d);
        float inv = float(1.0 / std::sqrt(var + eps));
        float* yi = y + i * d;
        for (int64_t j = 0; j < d; ++j) yi[j] = (xi[j] - float(mean)) * inv * g[size_t(j)] + b[size_t(j)];
    }
}

inline float gelu(float x) {
    return 0.5f * x * (1.0f + std::erf(x * 0.70710678118654752f));
}

inline float dot(const float* a, const float* b, int64_t n) {
    float acc[8] = {0, 0, 0, 0, 0, 0, 0, 0};
    int64_t k = 0;
    for (; k + 8 <= n; k += 8)
        for (int l = 0; l < 8; ++l) acc[l] += a[k + l] * b[k + l];
    float s = 0;
    for (int l = 0; l < 8; ++l) s += acc[l];
    for (; k < n; ++k) s += a[k] * b[k];
    return s;
}

// ---------------------------------------------------------- safetensors

class Safetensors {
public:
    explicit Safetensors(const std::string& path) {
        fd_ = ::open(path.c_str(), O_RDONLY);
        if (fd_ < 0) throw std::runtime_error("cannot open " + path);
        struct stat st {};
        if (::fstat(fd_, &st) != 0 || st.st_size < 8) throw std::runtime_error("bad safetensors file " + path);
        size_ = size_t(st.st_size);
        base_ = static_cast<const uint8_t*>(::mmap(nullptr, size_, PROT_READ, MAP_PRIVATE, fd_, 0));
        if (base_ == MAP_FAILED) {
            base_ = nullptr;
            throw std::runtime_error("cannot map " + path);
        }
        uint64_t hlen = 0;
        for (int i = 7; i >= 0; --i) hlen = (hlen << 8) | base_[i];
        if (hlen == 0 || 8 + hlen > size_) throw std::runtime_error("bad safetensors header in " + path);
        auto header = nlohmann::json::parse(base_ + 8, base_ + 8 + hlen);
        data_ = base_ + 8 + hlen;
        data_size_ = size_ - size_t(8 + hlen);
        for (auto it = header.begin(); it != header.end(); ++it) {
            if (it.key() == "__metadata__") continue;
            Entry e;
            e.dtype = it.value().at("dtype").get<std::string>();
            e.shape = it.value().at("shape").get<std::vector<int64_t>>();
            auto off = it.value().at("data_offsets").get<std::vector<uint64_t>>();
            if (off.size() != 2 || off[1] < off[0] || off[1] > data_size_)
                throw std::runtime_error("bad data offsets for " + it.key());
            e.begin = size_t(off[0]);
            e.end = size_t(off[1]);
            entries_[it.key()] = std::move(e);
        }
    }

    ~Safetensors() {
        if (base_) ::munmap(const_cast<uint8_t*>(base_), size_);
        if (fd_ >= 0) ::close(fd_);
    }
    Safetensors(const Safetensors&) = delete;
    Safetensors& operator=(const Safetensors&) = delete;

    Vec vec(const std::string& name, int64_t n) const { return load(name, {n}); }

    Mat mat(const std::string& name, int64_t rows, int64_t cols) const {
        Mat m(rows, cols);
        m.d = load(name, {rows, cols});
        return m;
    }

    float scalar(const std::string& name) const { return load(name, {}).at(0); }

private:
    struct Entry {
        std::string dtype;
        std::vector<int64_t> shape;
        size_t begin = 0, end = 0;
    };

    Vec load(const std::string& name, const std::vector<int64_t>& shape) const {
        auto it = entries_.find(name);
        if (it == entries_.end()) throw std::runtime_error("head tensor missing: " + name);
        const Entry& e = it->second;
        if (e.shape != shape) throw std::runtime_error("head tensor has an unexpected shape: " + name);
        int64_t n = 1;
        for (int64_t s : shape) n *= s;
        Vec out(static_cast<size_t>(n));
        const uint8_t* p = data_ + e.begin;
        if (e.dtype == "BF16") {
            if (e.end - e.begin != size_t(n) * 2) throw std::runtime_error("bad tensor size: " + name);
            for (int64_t i = 0; i < n; ++i) {
                uint32_t bits = (uint32_t(p[2 * i]) | (uint32_t(p[2 * i + 1]) << 8)) << 16;
                std::memcpy(&out[size_t(i)], &bits, 4);
            }
        } else if (e.dtype == "F32") {
            if (e.end - e.begin != size_t(n) * 4) throw std::runtime_error("bad tensor size: " + name);
            std::memcpy(out.data(), p, size_t(n) * 4);
        } else {
            throw std::runtime_error("unsupported head tensor dtype " + e.dtype + " for " + name);
        }
        return out;
    }

    int fd_ = -1;
    size_t size_ = 0;
    const uint8_t* base_ = nullptr;
    const uint8_t* data_ = nullptr;
    size_t data_size_ = 0;
    std::unordered_map<std::string, Entry> entries_;
};

// ------------------------------------------------------------ the head

struct Norm {
    Vec g, b;
};

struct Attn {
    Mat in_w, out_w; // in_proj_weight [3D, D], out_proj.weight [D, D]
    Vec in_b, out_b;
};

struct EvidenceLayer {
    Norm query_norm, memory_norm, feedforward_norm;
    Attn attention;
    Mat ff1, ff2;
    Vec ff1_b, ff2_b;
};

struct DecoderLayer {
    Norm norm1, norm2, norm3;
    Attn self_attn, cross_attn;
    Mat linear1, linear2;
    Vec linear1_b, linear2_b;
};

// One question of the encoded record, with positions in the input sequence.
struct QuestionSpans {
    int type = 0; // 0 noul, 1 choice, 2 score
    int64_t question_start = 0, question_end = 0;
    std::vector<std::pair<int64_t, int64_t>> options;
};

class Head {
public:
    static constexpr int64_t kHeads = 16;
    static constexpr int kQuestionTypes = 3;

    Head(const std::string& path, int threads) : threads_(std::max(1, threads)) {
        Safetensors st(path);
        // Shapes are checked against the tensors while loading.
        hidden_ = 4096;
        width_ = 1024;
        feedforward_ = 4096;
        hidden_norm_ = {st.vec("hidden_norm.weight", hidden_), st.vec("hidden_norm.bias", hidden_)};
        memory_projection_ = st.mat("memory_projection.weight", width_, hidden_);
        question_projection_ = st.mat("question_projection.weight", width_, hidden_);
        option_question_projection_ = st.mat("option_question_projection.weight", width_, hidden_);
        global_projection_ = st.mat("global_projection.weight", width_, hidden_);
        option_context_projection_ = st.mat("option_context_projection.weight", width_, hidden_);
        option_lexical_projection_ = st.mat("option_lexical_projection.weight", width_, hidden_);
        type_embedding_ = st.mat("type_embedding.weight", kQuestionTypes, width_);
        for (int i = 0; i < kRoutingLayers; ++i) {
            std::string p = "evidence_layers." + std::to_string(i) + ".";
            EvidenceLayer l;
            l.query_norm = norm(st, p + "query_norm");
            l.memory_norm = norm(st, p + "memory_norm");
            l.feedforward_norm = norm(st, p + "feedforward_norm");
            l.attention = attn(st, p + "attention");
            l.ff1 = st.mat(p + "feedforward.0.weight", feedforward_, width_);
            l.ff1_b = st.vec(p + "feedforward.0.bias", feedforward_);
            l.ff2 = st.mat(p + "feedforward.3.weight", width_, feedforward_);
            l.ff2_b = st.vec(p + "feedforward.3.bias", width_);
            evidence_.push_back(std::move(l));
        }
        option_summary_norm_ = norm(st, "option_summary_norm");
        for (int i = 0; i < kLayers; ++i) {
            std::string p = "layers." + std::to_string(i) + ".";
            DecoderLayer l;
            l.norm1 = norm(st, p + "norm1");
            l.norm2 = norm(st, p + "norm2");
            l.norm3 = norm(st, p + "norm3");
            l.self_attn = attn(st, p + "self_attn");
            l.cross_attn = attn(st, p + "multihead_attn");
            l.linear1 = st.mat(p + "linear1.weight", feedforward_, width_);
            l.linear1_b = st.vec(p + "linear1.bias", feedforward_);
            l.linear2 = st.mat(p + "linear2.weight", width_, feedforward_);
            l.linear2_b = st.vec(p + "linear2.bias", width_);
            layers_.push_back(std::move(l));
        }
        field_norm_ = norm(st, "field_norm");
        option_norm_ = norm(st, "option_norm");
        scorer1_ = st.mat("residual_scorer.0.weight", width_, width_ * 4);
        scorer1_b_ = st.vec("residual_scorer.0.bias", width_);
        scorer2_ = st.mat("residual_scorer.3.weight", 1, width_).d;
        scorer2_b_ = st.vec("residual_scorer.3.bias", 1)[0];
        const float cap = std::log(100.0f);
        prior_scale_ = std::exp(std::min(st.scalar("prior_logit_scale"), cap));
        joint_scale_ = std::exp(std::min(st.scalar("joint_logit_scale"), cap));
        gate_ = 1.0f / (1.0f + std::exp(-st.scalar("residual_gate")));
    }

    int64_t hidden_size() const { return hidden_; }

    // hidden: [L, hidden_size] last-layer states of the backbone (modified in
    // place). lexical[q]: [n_options(q), hidden_size] mean output-embedding of
    // each option's tokens. Returns one logit vector per question.
    std::vector<Vec> forward(Vec& hidden, int64_t L, const std::vector<QuestionSpans>& qs, const std::vector<Vec>& lexical) const {
        if (L <= 0 || qs.empty()) throw std::runtime_error("empty record");
        const int64_t H = hidden_, D = width_;
        layer_norm(hidden.data(), L, H, hidden_norm_.g, hidden_norm_.b, hidden.data());
        const float* nh = hidden.data();

        Mat memory(L, D);
        linear(nh, L, H, memory_projection_.d.data(), D, nullptr, memory.d.data(), threads_);
        const float* global = nh + (L - 1) * H;

        const int64_t Q = int64_t(qs.size());
        Mat qvec(Q, H);
        int64_t total_opts = 0;
        for (int64_t q = 0; q < Q; ++q) {
            mean_span(nh, H, qs[size_t(q)].question_start, qs[size_t(q)].question_end, qvec.row(q));
            total_opts += int64_t(qs[size_t(q)].options.size());
        }
        Mat octx(total_opts, H), olex(total_opts, H);
        std::vector<int64_t> first(static_cast<size_t>(Q));
        {
            int64_t at = 0;
            for (int64_t q = 0; q < Q; ++q) {
                const auto& opts = qs[size_t(q)].options;
                first[size_t(q)] = at;
                if (lexical[size_t(q)].size() != opts.size() * size_t(H)) throw std::runtime_error("bad lexical vectors");
                for (size_t o = 0; o < opts.size(); ++o, ++at) {
                    mean_span(nh, H, opts[o].first, opts[o].second, octx.row(at));
                    std::memcpy(olex.row(at), lexical[size_t(q)].data() + o * size_t(H), size_t(H) * sizeof(float));
                }
            }
        }

        // Option queries: context + lexical + owning question projections.
        Mat queries(total_opts, D), tmp(total_opts, D), qproj(Q, D);
        linear(octx.d.data(), total_opts, H, option_context_projection_.d.data(), D, nullptr, queries.d.data(), threads_);
        linear(olex.d.data(), total_opts, H, option_lexical_projection_.d.data(), D, nullptr, tmp.d.data(), threads_);
        linear(qvec.d.data(), Q, H, option_question_projection_.d.data(), D, nullptr, qproj.d.data(), threads_);
        for (int64_t q = 0; q < Q; ++q)
            for (size_t o = 0; o < qs[size_t(q)].options.size(); ++o) {
                float* dst = queries.row(first[size_t(q)] + int64_t(o));
                const float* a = tmp.row(first[size_t(q)] + int64_t(o));
                const float* b = qproj.row(q);
                for (int64_t j = 0; j < D; ++j) dst[j] += a[j] + b[j];
            }

        for (const auto& layer : evidence_) {
            Mat nq(total_opts, D), nm(L, D);
            layer_norm(queries.d.data(), total_opts, D, layer.query_norm.g, layer.query_norm.b, nq.d.data());
            layer_norm(memory.d.data(), L, D, layer.memory_norm.g, layer.memory_norm.b, nm.d.data());
            Mat routed = attend(nq, nm, layer.attention);
            add(queries, routed);
            Mat fn(total_opts, D);
            layer_norm(queries.d.data(), total_opts, D, layer.feedforward_norm.g, layer.feedforward_norm.b, fn.d.data());
            Mat ff = feed(fn, layer.ff1, layer.ff1_b, layer.ff2, layer.ff2_b);
            add(queries, ff);
        }

        // Fields: question projection + weighted option summary + global + type.
        Mat fields(Q, D), summaries(Q, D), nsum(Q, D), gproj(1, D);
        linear(qvec.d.data(), Q, H, question_projection_.d.data(), D, nullptr, fields.d.data(), threads_);
        const float inv_sqrt_d = 1.0f / std::sqrt(float(D));
        for (int64_t q = 0; q < Q; ++q) {
            int64_t n = int64_t(qs[size_t(q)].options.size());
            Vec w(static_cast<size_t>(n));
            for (int64_t o = 0; o < n; ++o) w[size_t(o)] = dot(queries.row(first[size_t(q)] + o), fields.row(q), D) * inv_sqrt_d;
            softmax(w);
            float* s = summaries.row(q);
            std::fill(s, s + D, 0.f);
            for (int64_t o = 0; o < n; ++o) {
                const float* r = queries.row(first[size_t(q)] + o);
                for (int64_t j = 0; j < D; ++j) s[j] += w[size_t(o)] * r[j];
            }
        }
        layer_norm(summaries.d.data(), Q, D, option_summary_norm_.g, option_summary_norm_.b, nsum.d.data());
        linear(global, 1, H, global_projection_.d.data(), D, nullptr, gproj.d.data(), 1);
        for (int64_t q = 0; q < Q; ++q) {
            int t = qs[size_t(q)].type;
            if (t < 0 || t >= kQuestionTypes) throw std::runtime_error("bad question type");
            float* f = fields.row(q);
            const float* a = nsum.row(q);
            const float* b = gproj.row(0);
            const float* c = type_embedding_.row(t);
            for (int64_t j = 0; j < D; ++j) f[j] += a[j] + b[j] + c[j];
        }

        for (const auto& layer : layers_) {
            Mat n1(Q, D);
            layer_norm(fields.d.data(), Q, D, layer.norm1.g, layer.norm1.b, n1.d.data());
            add(fields, attend(n1, n1, layer.self_attn));
            layer_norm(fields.d.data(), Q, D, layer.norm2.g, layer.norm2.b, n1.d.data());
            add(fields, attend(n1, memory, layer.cross_attn));
            layer_norm(fields.d.data(), Q, D, layer.norm3.g, layer.norm3.b, n1.d.data());
            add(fields, feed(n1, layer.linear1, layer.linear1_b, layer.linear2, layer.linear2_b));
        }
        layer_norm(fields.d.data(), Q, D, field_norm_.g, field_norm_.b, fields.d.data());

        std::vector<Vec> out(static_cast<size_t>(Q));
        for (int64_t q = 0; q < Q; ++q) {
            const int64_t n = int64_t(qs[size_t(q)].options.size());
            // Prior: option lexical anchors against the question + global anchor.
            Vec anchor(static_cast<size_t>(H));
            for (int64_t j = 0; j < H; ++j) anchor[size_t(j)] = qvec.row(q)[j] + global[j];
            normalize(anchor.data(), H);
            Mat options(n, D);
            layer_norm(queries.row(first[size_t(q)]), n, D, option_norm_.g, option_norm_.b, options.d.data());
            const float* field = fields.row(q);
            Mat features(n, D * 4);
            Vec cosine(static_cast<size_t>(n)), prior(static_cast<size_t>(n));
            const float fnorm = std::max(norm2(field, D), 1e-8f);
            for (int64_t o = 0; o < n; ++o) {
                Vec lex(olex.row(first[size_t(q)] + o), olex.row(first[size_t(q)] + o) + H);
                normalize(lex.data(), H);
                prior[size_t(o)] = prior_scale_ * dot(lex.data(), anchor.data(), H);
                const float* opt = options.row(o);
                cosine[size_t(o)] = dot(field, opt, D) / (fnorm * std::max(norm2(opt, D), 1e-8f));
                float* f = features.row(o);
                for (int64_t j = 0; j < D; ++j) {
                    f[j] = field[j];
                    f[D + j] = opt[j];
                    f[2 * D + j] = field[j] * opt[j];
                    f[3 * D + j] = std::fabs(field[j] - opt[j]);
                }
            }
            Mat hid(n, D);
            linear(features.d.data(), n, D * 4, scorer1_.d.data(), D, scorer1_b_.data(), hid.d.data(), threads_);
            Vec logits(static_cast<size_t>(n));
            for (int64_t o = 0; o < n; ++o) {
                float* h = hid.row(o);
                for (int64_t j = 0; j < D; ++j) h[j] = gelu(h[j]);
                float residual = dot(h, scorer2_.data(), D) + scorer2_b_;
                float joint = joint_scale_ * cosine[size_t(o)] + residual;
                logits[size_t(o)] = prior[size_t(o)] + gate_ * joint;
            }
            out[size_t(q)] = std::move(logits);
        }
        return out;
    }

private:
    static constexpr int kRoutingLayers = 2;
    static constexpr int kLayers = 4;

    static Norm norm(const Safetensors& st, const std::string& p) {
        return Norm{st.vec(p + ".weight", 1024), st.vec(p + ".bias", 1024)};
    }

    static Attn attn(const Safetensors& st, const std::string& p) {
        Attn a;
        a.in_w = st.mat(p + ".in_proj_weight", 3 * 1024, 1024);
        a.in_b = st.vec(p + ".in_proj_bias", 3 * 1024);
        a.out_w = st.mat(p + ".out_proj.weight", 1024, 1024);
        a.out_b = st.vec(p + ".out_proj.bias", 1024);
        return a;
    }

    static void mean_span(const float* x, int64_t d, int64_t s, int64_t e, float* out) {
        if (s < 0 || e <= s) throw std::runtime_error("empty span");
        std::fill(out, out + d, 0.f);
        for (int64_t i = s; i < e; ++i) {
            const float* r = x + i * d;
            for (int64_t j = 0; j < d; ++j) out[j] += r[j];
        }
        const float inv = 1.0f / float(e - s);
        for (int64_t j = 0; j < d; ++j) out[j] *= inv;
    }

    static float norm2(const float* x, int64_t n) { return std::sqrt(dot(x, x, n)); }

    static void normalize(float* x, int64_t n) {
        float inv = 1.0f / std::max(norm2(x, n), 1e-12f);
        for (int64_t i = 0; i < n; ++i) x[i] *= inv;
    }

    static void softmax(Vec& v) {
        float m = *std::max_element(v.begin(), v.end());
        double s = 0;
        for (auto& x : v) s += (x = std::exp(x - m));
        for (auto& x : v) x = float(x / s);
    }

    static void add(Mat& a, const Mat& b) {
        for (size_t i = 0; i < a.d.size(); ++i) a.d[i] += b.d[i];
    }

    Mat feed(const Mat& x, const Mat& w1, const Vec& b1, const Mat& w2, const Vec& b2) const {
        Mat h(x.r, w1.r), y(x.r, w2.r);
        linear(x.d.data(), x.r, x.c, w1.d.data(), w1.r, b1.data(), h.d.data(), threads_);
        for (auto& v : h.d) v = gelu(v);
        linear(h.d.data(), h.r, h.c, w2.d.data(), w2.r, b2.data(), y.d.data(), threads_);
        return y;
    }

    // nn.MultiheadAttention(q, kv, kv) with packed in-projection.
    Mat attend(const Mat& q_in, const Mat& kv_in, const Attn& a) const {
        const int64_t D = width_, Lq = q_in.r, Lk = kv_in.r, dh = D / kHeads;
        Mat q(Lq, D), k(Lk, D), v(Lk, D), ctx(Lq, D), out(Lq, D);
        linear(q_in.d.data(), Lq, D, a.in_w.d.data(), D, a.in_b.data(), q.d.data(), threads_);
        linear(kv_in.d.data(), Lk, D, a.in_w.d.data() + D * D, D, a.in_b.data() + D, k.d.data(), threads_);
        linear(kv_in.d.data(), Lk, D, a.in_w.d.data() + 2 * D * D, D, a.in_b.data() + 2 * D, v.d.data(), threads_);
        const float scale = 1.0f / std::sqrt(float(dh));
        parallel_for(Lq * kHeads, threads_, [&](int64_t b, int64_t e) {
            Vec s(static_cast<size_t>(Lk));
            for (int64_t idx = b; idx < e; ++idx) {
                const int64_t i = idx / kHeads, h = idx % kHeads;
                const float* qi = q.row(i) + h * dh;
                for (int64_t j = 0; j < Lk; ++j) s[size_t(j)] = dot(qi, k.row(j) + h * dh, dh) * scale;
                softmax(s);
                float* o = ctx.row(i) + h * dh;
                std::fill(o, o + dh, 0.f);
                for (int64_t j = 0; j < Lk; ++j) {
                    const float* vj = v.row(j) + h * dh;
                    const float w = s[size_t(j)];
                    for (int64_t d = 0; d < dh; ++d) o[d] += w * vj[d];
                }
            }
        });
        linear(ctx.d.data(), Lq, D, a.out_w.d.data(), D, a.out_b.data(), out.d.data(), threads_);
        return out;
    }

    int threads_;
    int64_t hidden_ = 0, width_ = 0, feedforward_ = 0;
    Norm hidden_norm_, option_summary_norm_, field_norm_, option_norm_;
    Mat memory_projection_, question_projection_, option_question_projection_, global_projection_;
    Mat option_context_projection_, option_lexical_projection_, type_embedding_;
    std::vector<EvidenceLayer> evidence_;
    std::vector<DecoderLayer> layers_;
    Mat scorer1_;
    Vec scorer1_b_, scorer2_;
    float scorer2_b_ = 0;
    float prior_scale_ = 1, joint_scale_ = 1, gate_ = 1;
};

} // namespace clef
