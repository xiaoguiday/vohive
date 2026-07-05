package swu

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/iniwex5/vowifi-go/engine/swu/ikev2"
)

func TestLinuxXFRMManagerApplyAndCleanup(t *testing.T) {
	runner := &fakeIPRunner{}
	manager := LinuxXFRMManager{Runner: runner}
	state, err := manager.Apply(context.Background(), KernelXFRMConfig{
		ChildSA:              xfrmChildSA(ikev2.INTEG_HMAC_SHA2_256_128),
		OuterLocalIP:         "192.0.2.23",
		OuterRemoteIP:        "198.51.100.7",
		InnerLocalPrefix:     "10.10.0.2/32",
		InnerRemotePrefix:    "10.20.0.0/24",
		ReqID:                77,
		Mark:                 "0x1/0xffffffff",
		IncludeForwardPolicy: true,
		XFRMInterface: XFRMInterfaceConfig{
			Name:     "ipsec0",
			OuterDev: "wwan0",
			IfID:     42,
			MTU:      1360,
		},
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	wantApply := [][]string{
		{"link", "add", "ipsec0", "type", "xfrm", "dev", "wwan0", "if_id", "0x2a"},
		{"link", "set", "dev", "ipsec0", "mtu", "1360"},
		{"link", "set", "dev", "ipsec0", "up"},
		{"xfrm", "state", "add", "src", "192.0.2.23", "dst", "198.51.100.7", "proto", "esp", "spi", "0xdeadbeef", "reqid", "77", "mode", "tunnel", "auth-trunc", "hmac(sha256)", xfrmHexKey(bytes.Repeat([]byte{0x20}, 32)), "128", "enc", "cbc(aes)", xfrmHexKey(bytes.Repeat([]byte{0x10}, 16)), "mark", "0x1/0xffffffff", "if_id", "0x2a"},
		{"xfrm", "state", "add", "src", "198.51.100.7", "dst", "192.0.2.23", "proto", "esp", "spi", "0xcafebabe", "reqid", "77", "mode", "tunnel", "auth-trunc", "hmac(sha256)", xfrmHexKey(bytes.Repeat([]byte{0x40}, 32)), "128", "enc", "cbc(aes)", xfrmHexKey(bytes.Repeat([]byte{0x30}, 16)), "mark", "0x1/0xffffffff", "if_id", "0x2a"},
		{"xfrm", "policy", "add", "src", "10.10.0.2/32", "dst", "10.20.0.0/24", "dir", "out", "mark", "0x1/0xffffffff", "if_id", "0x2a", "tmpl", "src", "192.0.2.23", "dst", "198.51.100.7", "proto", "esp", "reqid", "77", "mode", "tunnel"},
		{"xfrm", "policy", "add", "src", "10.20.0.0/24", "dst", "10.10.0.2/32", "dir", "in", "mark", "0x1/0xffffffff", "if_id", "0x2a", "tmpl", "src", "198.51.100.7", "dst", "192.0.2.23", "proto", "esp", "reqid", "77", "mode", "tunnel"},
		{"xfrm", "policy", "add", "src", "10.20.0.0/24", "dst", "10.10.0.2/32", "dir", "fwd", "mark", "0x1/0xffffffff", "if_id", "0x2a", "tmpl", "src", "198.51.100.7", "dst", "192.0.2.23", "proto", "esp", "reqid", "77", "mode", "tunnel"},
	}
	if !reflect.DeepEqual(runner.commands, wantApply) {
		t.Fatalf("apply commands=\n%v\nwant\n%v", runner.commands, wantApply)
	}
	if err := manager.Cleanup(context.Background(), state); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	wantAll := append([][]string{}, wantApply...)
	wantAll = append(wantAll,
		[]string{"xfrm", "policy", "delete", "src", "10.20.0.0/24", "dst", "10.10.0.2/32", "dir", "fwd", "mark", "0x1/0xffffffff", "if_id", "0x2a"},
		[]string{"xfrm", "policy", "delete", "src", "10.20.0.0/24", "dst", "10.10.0.2/32", "dir", "in", "mark", "0x1/0xffffffff", "if_id", "0x2a"},
		[]string{"xfrm", "policy", "delete", "src", "10.10.0.2/32", "dst", "10.20.0.0/24", "dir", "out", "mark", "0x1/0xffffffff", "if_id", "0x2a"},
		[]string{"xfrm", "state", "delete", "src", "198.51.100.7", "dst", "192.0.2.23", "proto", "esp", "spi", "0xcafebabe", "mark", "0x1/0xffffffff", "if_id", "0x2a"},
		[]string{"xfrm", "state", "delete", "src", "192.0.2.23", "dst", "198.51.100.7", "proto", "esp", "spi", "0xdeadbeef", "mark", "0x1/0xffffffff", "if_id", "0x2a"},
		[]string{"link", "del", "ipsec0"},
	)
	if !reflect.DeepEqual(runner.commands, wantAll) {
		t.Fatalf("all commands=\n%v\nwant\n%v", runner.commands, wantAll)
	}
}

func TestLinuxXFRMManagerRollsBackOnFailure(t *testing.T) {
	wantErr := errors.New("policy failed")
	runner := &fakeIPRunner{failAt: 3, err: wantErr}
	manager := LinuxXFRMManager{Runner: runner}
	_, err := manager.Apply(context.Background(), KernelXFRMConfig{
		ChildSA:           xfrmChildSA(ikev2.INTEG_HMAC_SHA2_256_128),
		OuterLocalIP:      "192.0.2.23",
		OuterRemoteIP:     "198.51.100.7",
		InnerLocalPrefix:  "10.10.0.2/32",
		InnerRemotePrefix: "10.20.0.0/24",
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Apply() err=%v, want policy failure", err)
	}
	want := [][]string{
		{"xfrm", "state", "add", "src", "192.0.2.23", "dst", "198.51.100.7", "proto", "esp", "spi", "0xdeadbeef", "reqid", "1", "mode", "tunnel", "auth-trunc", "hmac(sha256)", xfrmHexKey(bytes.Repeat([]byte{0x20}, 32)), "128", "enc", "cbc(aes)", xfrmHexKey(bytes.Repeat([]byte{0x10}, 16))},
		{"xfrm", "state", "add", "src", "198.51.100.7", "dst", "192.0.2.23", "proto", "esp", "spi", "0xcafebabe", "reqid", "1", "mode", "tunnel", "auth-trunc", "hmac(sha256)", xfrmHexKey(bytes.Repeat([]byte{0x40}, 32)), "128", "enc", "cbc(aes)", xfrmHexKey(bytes.Repeat([]byte{0x30}, 16))},
		{"xfrm", "policy", "add", "src", "10.10.0.2/32", "dst", "10.20.0.0/24", "dir", "out", "tmpl", "src", "192.0.2.23", "dst", "198.51.100.7", "proto", "esp", "reqid", "1", "mode", "tunnel"},
		{"xfrm", "state", "delete", "src", "198.51.100.7", "dst", "192.0.2.23", "proto", "esp", "spi", "0xcafebabe"},
		{"xfrm", "state", "delete", "src", "192.0.2.23", "dst", "198.51.100.7", "proto", "esp", "spi", "0xdeadbeef"},
	}
	if !reflect.DeepEqual(runner.commands, want) {
		t.Fatalf("commands=\n%v\nwant\n%v", runner.commands, want)
	}
}

func TestBuildKernelXFRMCommandsSupportsSHA1(t *testing.T) {
	commands, err := buildKernelXFRMCommands(KernelXFRMConfig{
		ChildSA:           xfrmChildSA(ikev2.INTEG_HMAC_SHA1_96),
		OuterLocalIP:      "192.0.2.23",
		OuterRemoteIP:     "198.51.100.7",
		InnerLocalPrefix:  "10.10.0.2",
		InnerRemotePrefix: "10.20.0.0/24",
	})
	if err != nil {
		t.Fatalf("buildKernelXFRMCommands() error = %v", err)
	}
	got := commands[0].args
	if !reflect.DeepEqual(got[15:19], []string{"auth-trunc", "hmac(sha1)", xfrmHexKey(bytes.Repeat([]byte{0x20}, 20)), "96"}) {
		t.Fatalf("auth args=%v", got[15:19])
	}
}

func TestBuildKernelXFRMCommandsRejectsInvalidInput(t *testing.T) {
	base := KernelXFRMConfig{
		ChildSA:           xfrmChildSA(ikev2.INTEG_HMAC_SHA2_256_128),
		OuterLocalIP:      "192.0.2.23",
		OuterRemoteIP:     "198.51.100.7",
		InnerLocalPrefix:  "10.10.0.2/32",
		InnerRemotePrefix: "10.20.0.0/24",
	}
	cases := []struct {
		name string
		cfg  KernelXFRMConfig
	}{
		{name: "bad outer", cfg: withXFRM(base, func(c *KernelXFRMConfig) { c.OuterLocalIP = "bad" })},
		{name: "bad inner", cfg: withXFRM(base, func(c *KernelXFRMConfig) { c.InnerLocalPrefix = "bad" })},
		{name: "bad reqid", cfg: withXFRM(base, func(c *KernelXFRMConfig) { c.ReqID = -1 })},
		{name: "bad mark", cfg: withXFRM(base, func(c *KernelXFRMConfig) { c.Mark = "bad mark" })},
		{name: "bad local spi", cfg: withXFRM(base, func(c *KernelXFRMConfig) { c.ChildSA.LocalSPI = []byte{1, 2} })},
		{name: "bad encryption", cfg: withXFRM(base, func(c *KernelXFRMConfig) { c.ChildSA.Keys.Profile.EncryptionID = ikev2.ENCR_AES_GCM_16 })},
		{name: "bad integrity", cfg: withXFRM(base, func(c *KernelXFRMConfig) { c.ChildSA.Keys.Profile.IntegrityID = ikev2.INTEG_AES_XCBC_96 })},
		{name: "bad key length", cfg: withXFRM(base, func(c *KernelXFRMConfig) { c.ChildSA.Keys.Outbound.EncryptionKey = []byte{1, 2, 3} })},
		{name: "xfrmi no ifid", cfg: withXFRM(base, func(c *KernelXFRMConfig) { c.XFRMInterface = XFRMInterfaceConfig{Name: "ipsec0", OuterDev: "wwan0"} })},
		{name: "xfrmi bad outer dev", cfg: withXFRM(base, func(c *KernelXFRMConfig) {
			c.XFRMInterface = XFRMInterfaceConfig{Name: "ipsec0", OuterDev: "bad dev", IfID: 1}
		})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildKernelXFRMCommands(tc.cfg)
			if !errors.Is(err, ErrInvalidXFRMConfig) {
				t.Fatalf("buildKernelXFRMCommands() err=%v, want ErrInvalidXFRMConfig", err)
			}
		})
	}
}

func xfrmChildSA(integrity uint16) ikev2.ChildSAResult {
	integLen := 32
	if integrity == ikev2.INTEG_HMAC_SHA1_96 {
		integLen = 20
	}
	return ikev2.ChildSAResult{
		LocalSPI:  []byte{0xca, 0xfe, 0xba, 0xbe},
		RemoteSPI: []byte{0xde, 0xad, 0xbe, 0xef},
		Keys: ikev2.ChildSAKeys{
			Profile: ikev2.ESPKeyProfile{
				EncryptionID:        ikev2.ENCR_AES_CBC,
				EncryptionKeyLength: 16,
				IntegrityID:         integrity,
				IntegrityKeyLength:  integLen,
			},
			Outbound: ikev2.ESPKeys{
				EncryptionKey: bytes.Repeat([]byte{0x10}, 16),
				IntegrityKey:  bytes.Repeat([]byte{0x20}, integLen),
			},
			Inbound: ikev2.ESPKeys{
				EncryptionKey: bytes.Repeat([]byte{0x30}, 16),
				IntegrityKey:  bytes.Repeat([]byte{0x40}, integLen),
			},
		},
	}
}

func withXFRM(cfg KernelXFRMConfig, fn func(*KernelXFRMConfig)) KernelXFRMConfig {
	fn(&cfg)
	return cfg
}
