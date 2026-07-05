package device

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNormalizeATPorts(t *testing.T) {
	got := normalizeATPorts([]string{
		" /dev/ttyUSB7 ",
		"/dev/ttyUSB6",
		"",
		"/dev/ttyUSB7",
		"/dev/ttyUSB4",
	})

	want := []string{"/dev/ttyUSB4", "/dev/ttyUSB6", "/dev/ttyUSB7"}
	if len(got) != len(want) {
		t.Fatalf("len=%d want=%d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%q want=%q (all=%v)", i, got[i], want[i], got)
		}
	}
}

func TestChooseStaticATPortsPrefersHintWithinDevicePorts(t *testing.T) {
	primary, backup := chooseStaticATPorts(
		[]string{"/dev/ttyUSB6", "/dev/ttyUSB7", "/dev/ttyUSB4"},
		"/dev/ttyUSB7",
	)

	if primary != "/dev/ttyUSB7" {
		t.Fatalf("primary=%q want=%q", primary, "/dev/ttyUSB7")
	}
	if backup != "/dev/ttyUSB4" {
		t.Fatalf("backup=%q want=%q", backup, "/dev/ttyUSB4")
	}
}

func TestChooseStaticATPortsIgnoresHintOutsideDevicePorts(t *testing.T) {
	primary, backup := chooseStaticATPorts(
		[]string{"/dev/ttyUSB6", "/dev/ttyUSB7"},
		"/dev/ttyUSB4",
	)

	if primary != "/dev/ttyUSB6" {
		t.Fatalf("primary=%q want=%q", primary, "/dev/ttyUSB6")
	}
	if backup != "/dev/ttyUSB7" {
		t.Fatalf("backup=%q want=%q", backup, "/dev/ttyUSB7")
	}
}

func TestFindATPortsCollectsBothTTYLayouts(t *testing.T) {
	usbPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(usbPath, "1-2:1.2", "ttyUSB6"), 0o755); err != nil {
		t.Fatalf("mkdir direct tty layout: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(usbPath, "1-2:1.3", "tty", "ttyUSB7"), 0o755); err != nil {
		t.Fatalf("mkdir nested tty layout: %v", err)
	}

	got := findATPorts(usbPath)
	want := []string{"/dev/ttyUSB6", "/dev/ttyUSB7"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findATPorts()=%v want=%v", got, want)
	}
}

func TestDiscoverFromSysFSStaticTopologyOnly(t *testing.T) {
	usbPath := t.TempDir()
	usbName := filepath.Base(usbPath)

	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(usbPath, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	write("idVendor", "2c7c\n")
	write("idProduct", "0125\n")
	write("bNumInterfaces", "5\n")

	ifacePath := filepath.Join(usbPath, usbName+":1.4")
	if err := os.MkdirAll(filepath.Join(ifacePath, "net", "wwan9"), 0o755); err != nil {
		t.Fatalf("mkdir net iface: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(ifacePath, "usbmisc", "cdc-wdm9"), 0o755); err != nil {
		t.Fatalf("mkdir cdc-wdm tree: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(usbPath, usbName+":1.2", "tty", "ttyUSB6"), 0o755); err != nil {
		t.Fatalf("mkdir at port tree: %v", err)
	}
	if err := os.Symlink("/tmp/qmi_wwan", filepath.Join(ifacePath, "driver")); err != nil {
		t.Fatalf("symlink driver: %v", err)
	}

	got, err := discoverFromSysFS(usbPath)
	if err != nil {
		t.Fatalf("discoverFromSysFS() error = %v", err)
	}
	if got == nil {
		t.Fatal("discoverFromSysFS() returned nil")
	}
	if got.ControlPath != "/dev/cdc-wdm9" {
		t.Fatalf("ControlPath=%q want %q", got.ControlPath, "/dev/cdc-wdm9")
	}
	if got.ATPort != "/dev/ttyUSB6" {
		t.Fatalf("ATPort=%q want %q", got.ATPort, "/dev/ttyUSB6")
	}
}
