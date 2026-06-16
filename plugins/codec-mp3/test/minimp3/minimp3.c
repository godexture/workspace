#define MINIMP3_FLOAT_OUTPUT
#define MINIMP3_IMPLEMENTATION

#include "minimp3.h"
#include "minimp3_ex.h"

int mp3dec_skip_id3_bytes(const uint8_t *buf, int size) {
    const uint8_t *p = buf;
    size_t sz = size;
    mp3dec_skip_id3(&p, &sz);
    return (int)(p - buf);
}

void mp3dec_synth_granule_c(float *qmf_state, float *grbuf, int nbands, int nch, float *pcm, float *lins) {
    mp3d_synth_granule(qmf_state, grbuf, nbands, nch, pcm, lins);
}

void mp3dec_imdct_gr_c(float *grbuf, float *overlap, int block_type, int n_long_bands) {
    L3_imdct_gr(grbuf, overlap, (unsigned)block_type, (unsigned)n_long_bands);
}

void mp3dec_huffman_c(float *dst, void *bs, const void *gr_info, const float *scf, int layer3gr_limit) {
    L3_huffman(dst, (bs_t *)bs, (const L3_gr_info_t *)gr_info, scf, layer3gr_limit);
}
