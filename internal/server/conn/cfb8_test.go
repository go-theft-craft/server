package conn

import (
	"bytes"
	"crypto/aes"
	"crypto/rand"
	"testing"
)

func TestCFB8RoundTrip(t *testing.T) {
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	iv := make([]byte, 16)
	copy(iv, key) // Minecraft uses key=IV

	plaintext := []byte("Hello, Minecraft encryption test! This is longer than 16 bytes.")

	// Encrypt
	blockEnc, _ := aes.NewCipher(key)
	enc := newCFB8Encrypt(blockEnc, iv)
	ciphertext := make([]byte, len(plaintext))
	enc.XORKeyStream(ciphertext, plaintext)

	// Ciphertext should differ from plaintext.
	if bytes.Equal(ciphertext, plaintext) {
		t.Error("ciphertext equals plaintext")
	}

	// Decrypt
	blockDec, _ := aes.NewCipher(key)
	dec := newCFB8Decrypt(blockDec, iv)
	recovered := make([]byte, len(ciphertext))
	dec.XORKeyStream(recovered, ciphertext)

	if !bytes.Equal(recovered, plaintext) {
		t.Errorf("decrypted text does not match plaintext\ngot:  %x\nwant: %x", recovered, plaintext)
	}
}

func TestCFB8ByteAtATime(t *testing.T) {
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	iv := make([]byte, 16)
	copy(iv, key)

	plaintext := []byte("byte-at-a-time test")

	// Encrypt all at once.
	blockEnc, _ := aes.NewCipher(key)
	enc := newCFB8Encrypt(blockEnc, iv)
	cipherAll := make([]byte, len(plaintext))
	enc.XORKeyStream(cipherAll, plaintext)

	// Encrypt byte-at-a-time.
	blockEnc2, _ := aes.NewCipher(key)
	enc2 := newCFB8Encrypt(blockEnc2, iv)
	cipherByByte := make([]byte, len(plaintext))
	for i := range plaintext {
		enc2.XORKeyStream(cipherByByte[i:i+1], plaintext[i:i+1])
	}

	if !bytes.Equal(cipherAll, cipherByByte) {
		t.Errorf("byte-at-a-time encryption differs from batch\nbatch:  %x\nbyte:   %x", cipherAll, cipherByByte)
	}
}
