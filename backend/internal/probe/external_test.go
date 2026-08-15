package probe

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestValidateExternalURLBlocksPrivateIPButKeepsSyntaxValid(t *testing.T) {
	if err := ValidateExternalURL("http://127.0.0.1:8080"); !errors.Is(err, ErrBlockedTarget) {
		t.Fatalf("loopback error = %v", err)
	}
	if err := ValidateExternalURL("http://192.168.1.11:8080"); !errors.Is(err, ErrBlockedTarget) {
		t.Fatalf("private error = %v", err)
	}
	if err := ValidateExternalURL("https://example.com"); err != nil {
		t.Fatalf("public hostname error = %v", err)
	}
	if err := ValidateExternalURL("https://example.com:65536"); err == nil {
		t.Fatal("invalid port accepted")
	}
}

func TestSafeDialContextRejectsLocalAddress(t *testing.T) {
	dialer := &net.Dialer{Timeout: 100 * time.Millisecond}
	_, err := safeDialContext(context.Background(), dialer, "tcp", "127.0.0.1:80")
	if !errors.Is(err, ErrBlockedTarget) {
		t.Fatalf("safe dial error = %v", err)
	}
}
