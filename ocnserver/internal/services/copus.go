package services

/*
#cgo LDFLAGS: -lopus
#include <opus/opus.h>
#include <stdlib.h>

static OpusEncoder* c_enc_create(int rate, int channels) {
	int err = 0;
	return opus_encoder_create(rate, channels, OPUS_APPLICATION_VOIP, &err);
}
static int c_enc_encode(OpusEncoder* e, const opus_int16* pcm, int frame, unsigned char* out, int cap) {
	return opus_encode(e, pcm, frame, out, cap);
}
static void c_enc_destroy(OpusEncoder* e) { if (e) opus_encoder_destroy(e); }

static OpusDecoder* c_dec_create(int rate, int channels) {
	int err = 0;
	return opus_decoder_create(rate, channels, &err);
}
static int c_dec_decode(OpusDecoder* d, const unsigned char* in, int len, opus_int16* pcm, int frame) {
	return opus_decode(d, in, len, pcm, frame, 0);
}
static void c_dec_destroy(OpusDecoder* d) { if (d) opus_decoder_destroy(d); }
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// OpusEncoder wraps a libopus encoder (mono, 48kHz fixed for this mixer).
type OpusEncoder struct{ c *C.OpusEncoder }

func NewOpusEncoder() (*OpusEncoder, error) {
	e := C.c_enc_create(C.int(mixSampleRate), 1)
	if e == nil {
		return nil, fmt.Errorf("opus encoder create failed")
	}
	return &OpusEncoder{c: e}, nil
}

func (e *OpusEncoder) Encode(pcm []int16, out []byte) (int, error) {
	if len(out) == 0 {
		return 0, fmt.Errorf("empty output buffer")
	}
	var pcmPtr *C.opus_int16
	if len(pcm) > 0 {
		pcmPtr = (*C.opus_int16)(unsafe.Pointer(&pcm[0]))
	}
	n := C.c_enc_encode(e.c, pcmPtr, C.int(len(pcm)),
		(*C.uchar)(unsafe.Pointer(&out[0])), C.int(len(out)))
	if n < 0 {
		return 0, fmt.Errorf("opus encode error %d", int(n))
	}
	return int(n), nil
}

func (e *OpusEncoder) Close() {
	if e != nil && e.c != nil {
		C.c_enc_destroy(e.c)
		e.c = nil
	}
}

// OpusDecoder wraps a libopus decoder (mono, 48kHz).
type OpusDecoder struct{ c *C.OpusDecoder }

func NewOpusDecoder() (*OpusDecoder, error) {
	d := C.c_dec_create(C.int(mixSampleRate), 1)
	if d == nil {
		return nil, fmt.Errorf("opus decoder create failed")
	}
	return &OpusDecoder{c: d}, nil
}

// Decode decodes one frame (in) into pcm samples. Returns samples decoded.
func (d *OpusDecoder) Decode(in []byte, pcm []int16) (int, error) {
	if len(in) == 0 {
		return 0, fmt.Errorf("empty opus packet")
	}
	var pcmPtr *C.opus_int16
	if len(pcm) > 0 {
		pcmPtr = (*C.opus_int16)(unsafe.Pointer(&pcm[0]))
	}
	n := C.c_dec_decode(d.c, (*C.uchar)(unsafe.Pointer(&in[0])), C.int(len(in)),
		pcmPtr, C.int(len(pcm)))
	if n < 0 {
		return 0, fmt.Errorf("opus decode error %d", int(n))
	}
	return int(n), nil
}

func (d *OpusDecoder) Close() {
	if d != nil && d.c != nil {
		C.c_dec_destroy(d.c)
		d.c = nil
	}
}
