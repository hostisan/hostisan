// Package totp holds the two-factor helpers.
package totp

import (
	"encoding/base64"

	"rsc.io/qr"
)

// PNGDataURI encodes text as a QR code and returns it as a PNG data URI for an <img>.
func PNGDataURI(text string) (string, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(code.PNG()), nil
}
