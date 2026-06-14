#define MINIMP3_IMPLEMENTATION
#ifndef MINIMP3_FLOAT_OUTPUT
#define MINIMP3_FLOAT_OUTPUT
#endif
#include "minimp3.h"
#include "minimp3_ex.h"

int mp3dec_skip_id3_bytes(const uint8_t *buf, int size) {
    const uint8_t *p = buf;
    size_t sz = size;
    mp3dec_skip_id3(&p, &sz);
    return (int)(p - buf);
}
