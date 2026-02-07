package conn

import "crypto/cipher"

// cfb8Stream implements CFB8 mode (8-bit feedback) used by Minecraft.
// Both encrypt and decrypt use the block cipher's Encrypt method;
// the difference is which byte (plaintext or ciphertext) is shifted
// into the feedback register.
type cfb8Stream struct {
	block   cipher.Block
	iv      [16]byte
	encrypt bool
}

func newCFB8Encrypt(block cipher.Block, iv []byte) *cfb8Stream {
	s := &cfb8Stream{block: block, encrypt: true}
	copy(s.iv[:], iv)
	return s
}

func newCFB8Decrypt(block cipher.Block, iv []byte) *cfb8Stream {
	s := &cfb8Stream{block: block, encrypt: false}
	copy(s.iv[:], iv)
	return s
}

func (s *cfb8Stream) XORKeyStream(dst, src []byte) {
	var tmp [16]byte
	for i, b := range src {
		s.block.Encrypt(tmp[:], s.iv[:])
		outByte := b ^ tmp[0]

		if s.encrypt {
			// Encrypt: feedback the ciphertext byte.
			dst[i] = outByte
			s.shiftIn(outByte)
		} else {
			// Decrypt: feedback the ciphertext byte (which is the input byte).
			s.shiftIn(b)
			dst[i] = outByte
		}
	}
}

// shiftIn shifts the IV left by 1 byte and appends b.
func (s *cfb8Stream) shiftIn(b byte) {
	copy(s.iv[:], s.iv[1:])
	s.iv[15] = b
}
