package totp

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestPNGDataURI(t *testing.T) {
	uri, err := PNGDataURI("otpauth://totp/Hostisan:bob?secret=JBSWY3DPEHPK3PXP&issuer=Hostisan")
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(uri, prefix) {
		t.Fatalf("uri = %.40q", uri)
	}
	png, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(uri, prefix))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(png, []byte("\x89PNG\r\n\x1a\n")) {
		t.Errorf("decoded bytes don't start with the PNG signature: % x", png[:8])
	}
}
