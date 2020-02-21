package transport

import (
	"fmt"
	"testing"
)

func WriteTrunc(data []byte) error {
	size := 17
	i := 0

	for ; i < len(data)/size; i++ {
		fmt.Printf("%v\n", string(data[size*i:i*size+size]))
	}

	fmt.Printf("%v\n", string(data[i*size:]))
	return nil
}

func TestTrunc(t *testing.T) {
	// data := []byte("1234567890123456789012345678901234567890")
	data := []byte("123456")
	WriteTrunc(data)
}
