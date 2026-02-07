package conn

import (
	"crypto/aes"
	"fmt"
	"net"
)

// encryptedConn wraps a net.Conn with AES/CFB8 encryption and decryption.
// Minecraft uses the same shared secret as both key and IV, with separate
// CFB8 streams for reading and writing.
type encryptedConn struct {
	conn    net.Conn
	encrypt *cfb8Stream
	decrypt *cfb8Stream
}

func newEncryptedConn(conn net.Conn, sharedSecret []byte) (*encryptedConn, error) {
	block, err := aes.NewCipher(sharedSecret)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}

	// Key = IV = sharedSecret for both directions.
	encStream := newCFB8Encrypt(block, sharedSecret)

	// Decrypt needs its own block cipher and IV copy.
	block2, _ := aes.NewCipher(sharedSecret)
	decStream := newCFB8Decrypt(block2, sharedSecret)

	return &encryptedConn{
		conn:    conn,
		encrypt: encStream,
		decrypt: decStream,
	}, nil
}

func (e *encryptedConn) Read(p []byte) (int, error) {
	n, err := e.conn.Read(p)
	if n > 0 {
		e.decrypt.XORKeyStream(p[:n], p[:n])
	}
	return n, err
}

func (e *encryptedConn) Write(p []byte) (int, error) {
	encrypted := make([]byte, len(p))
	e.encrypt.XORKeyStream(encrypted, p)
	return e.conn.Write(encrypted)
}
