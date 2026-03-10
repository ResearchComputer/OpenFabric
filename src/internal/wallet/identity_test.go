package wallet

import (
	"testing"
	"time"
)

func TestSignAndVerifyIdentity(t *testing.T) {
	dir := t.TempDir()
	wm, err := NewWalletManagerWithDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = wm.AddSolanaAccount()
	if err != nil {
		t.Fatal(err)
	}

	att, err := SignIdentity("QmTestPeer123", time.Now().Unix(), wm)
	if err != nil {
		t.Fatal(err)
	}

	if err := VerifyIdentity(att); err != nil {
		t.Fatalf("valid attestation should verify: %v", err)
	}
}

func TestVerifyIdentityRejectsTamperedPeerID(t *testing.T) {
	dir := t.TempDir()
	wm, err := NewWalletManagerWithDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = wm.AddSolanaAccount()
	if err != nil {
		t.Fatal(err)
	}

	att, err := SignIdentity("QmTestPeer123", time.Now().Unix(), wm)
	if err != nil {
		t.Fatal(err)
	}

	att.PeerID = "QmTamperedPeer"
	if err := VerifyIdentity(att); err == nil {
		t.Fatal("tampered peer ID should fail verification")
	}
}

func TestVerifyIdentityRejectsNil(t *testing.T) {
	if err := VerifyIdentity(nil); err == nil {
		t.Fatal("nil attestation should fail")
	}
}
