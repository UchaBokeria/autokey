package mail

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"
)

func TestVerifyWebhook(t *testing.T) {
	secret := "s3cret"
	body := []byte(`{"id":"e1","type":"card.created"}`)
	ts := fmt.Sprint(time.Now().Unix())
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%s.%s", ts, string(body))
	sig := "t=" + ts + ",v1=" + hex.EncodeToString(mac.Sum(nil))
	if err := VerifyKripiWebhook(secret, ts, sig, body, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyWebhookReplay(t *testing.T) {
	secret := "s3cret"
	body := []byte(`{}`)
	old := fmt.Sprint(time.Now().Add(-10 * time.Minute).Unix())
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%s.%s", old, string(body))
	sig := "t=" + old + ",v1=" + hex.EncodeToString(mac.Sum(nil))
	if err := VerifyKripiWebhook(secret, old, sig, body, time.Now()); err == nil {
		t.Fatal("want replay rejection")
	}
}

func TestVerifyWebhookTampered(t *testing.T) {
	secret := "s3cret"
	body := []byte(`{"a":1}`)
	ts := fmt.Sprint(time.Now().Unix())
	if err := VerifyKripiWebhook(secret, ts, "t="+ts+",v1=deadbeef", body, time.Now()); err == nil {
		t.Fatal("want signature rejection")
	}
}
