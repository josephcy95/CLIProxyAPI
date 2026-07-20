package helps

import (
	"encoding/base64"
)

// QoderCNEncodeBody encodes a request body using the Qoder body encoding scheme.
// This is a port of qoder2api's QoderEncoding.java. The algorithm:
//
//  1. Standard base64-encode the plaintext bytes.
//  2. Rearrange: split into thirds, reorder as [tail][mid][head].
//  3. Substitute each character using a custom alphabet mapping.
//
// The encoded body must be sent with &Encode=1 appended to the URL.
// The server decodes in reverse. This obfuscation prevents Alibaba Cloud WAF
// from pattern-matching the plaintext request body.
func QoderCNEncodeBody(plaintext []byte) string {
	std := base64.StdEncoding.EncodeToString(plaintext)
	n := len(std)
	a := n / 3
	// Rearrange: [tail][mid][head]
	rearranged := std[n-a:] + std[a:n-a] + std[:a]
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		c := rearranged[i]
		if int(c) < 128 && qodercnS2C[c] >= 0 {
			out[i] = byte(qodercnS2C[c])
		} else {
			out[i] = c
		}
	}
	return string(out)
}

const (
	qodercnCustomAlphabet = "_doRTgHZBKcGVjlvpC,@aFSx#DPuNJme&i*MzLOEn)sUrthbf%Y^w.(kIQyXqWA!"
	qodercnStdAlphabet    = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
)

// qodercnS2C maps standard base64 chars → custom alphabet chars.
var qodercnS2C [128]int

func init() {
	for i := range qodercnS2C {
		qodercnS2C[i] = -1
	}
	for i := 0; i < 64; i++ {
		qodercnS2C[qodercnStdAlphabet[i]] = int(qodercnCustomAlphabet[i])
	}
	qodercnS2C['='] = int('$') // custom pad
}
