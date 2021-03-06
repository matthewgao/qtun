package transport

import (
	"crypto/aes"
	"crypto/cipher"

	"github.com/matthewgao/qtun/utils"
)

func makeAES128GCM(key string) (cipher.AEAD, error) {
	bkey := utils.MD5([]byte(key))
	block, err := aes.NewCipher(bkey)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
	// return cipher.NewGCMWithNonceSize(block, 16)
}

func makeAES256GCM(key string) (cipher.AEAD, error) {
	bkey := utils.SHA256([]byte(key))
	block, err := aes.NewCipher(bkey)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
