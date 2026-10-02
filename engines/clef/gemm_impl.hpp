// Dense float32 kernels for the Clef head. This file is included twice from
// head.hpp (generic and AVX2+FMA targets) and intentionally has no include
// guard; it must be included inside a namespace.

// One MR x NR block of C = A * B^T where A is [MR, K] (leading dim lda) and B
// is [NR, K] (leading dim ldb): lane-wise partial sums vectorise cleanly.
template <int MR, int NR>
static inline void tile(const float* A, int64_t lda, const float* B, int64_t ldb, int64_t K, float* C, int64_t ldc) {
    constexpr int L = 8;
    float acc[MR][NR][L];
    for (int i = 0; i < MR; ++i)
        for (int j = 0; j < NR; ++j)
            for (int l = 0; l < L; ++l) acc[i][j][l] = 0.f;
    int64_t k = 0;
    for (; k + L <= K; k += L)
        for (int i = 0; i < MR; ++i)
            for (int j = 0; j < NR; ++j)
                for (int l = 0; l < L; ++l) acc[i][j][l] += A[i * lda + k + l] * B[j * ldb + k + l];
    for (int i = 0; i < MR; ++i)
        for (int j = 0; j < NR; ++j) {
            float s = 0.f;
            for (int l = 0; l < L; ++l) s += acc[i][j][l];
            for (int64_t kk = k; kk < K; ++kk) s += A[i * lda + kk] * B[j * ldb + kk];
            C[i * ldc + j] = s;
        }
}

static inline void tile_dyn(int mr, int nr, const float* A, int64_t lda, const float* B, int64_t ldb, int64_t K, float* C,
                            int64_t ldc) {
    switch (mr * 10 + nr) {
    case 42: tile<4, 2>(A, lda, B, ldb, K, C, ldc); break;
    case 41: tile<4, 1>(A, lda, B, ldb, K, C, ldc); break;
    case 32: tile<3, 2>(A, lda, B, ldb, K, C, ldc); break;
    case 31: tile<3, 1>(A, lda, B, ldb, K, C, ldc); break;
    case 22: tile<2, 2>(A, lda, B, ldb, K, C, ldc); break;
    case 21: tile<2, 1>(A, lda, B, ldb, K, C, ldc); break;
    case 12: tile<1, 2>(A, lda, B, ldb, K, C, ldc); break;
    default: tile<1, 1>(A, lda, B, ldb, K, C, ldc); break;
    }
}

// C[m, n] = dot(A[m, :], B[n, :]) for every m < M and n in [n0, n1).
// A is [M, K], B is [N, K] (row = output feature), C has leading dim ldc.
static void gemm(const float* A, int64_t M, int64_t K, const float* B, int64_t n0, int64_t n1, float* C, int64_t ldc) {
    constexpr int64_t MB = 64; // rows of A kept hot while B streams
    for (int64_t m0 = 0; m0 < M; m0 += MB) {
        int64_t m1 = std::min(M, m0 + MB);
        for (int64_t n = n0; n < n1; n += 2) {
            int nr = int(std::min<int64_t>(2, n1 - n));
            int64_t m = m0;
            for (; m + 4 <= m1; m += 4) tile_dyn(4, nr, A + m * K, K, B + n * K, K, K, C + m * ldc + n, ldc);
            if (m < m1) tile_dyn(int(m1 - m), nr, A + m * K, K, B + n * K, K, K, C + m * ldc + n, ldc);
        }
    }
}
