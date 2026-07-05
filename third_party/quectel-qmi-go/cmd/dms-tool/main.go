package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
)

func main() {
	devicePath := flag.String("device", defaultQmiDevice(), "Path to QMI device")
	action := flag.String("action", "all", "Action: all, sim, pin, uim, mode, serial, revision, dump")
	flag.Parse()

	client, err := qmi.NewClientWithOptions(context.Background(), *devicePath, qmi.ClientOptions{})
	if err != nil {
		log.Fatalf("Failed to create QMI client: %v", err)
	}
	defer client.Close()

	dms, err := qmi.NewDMSService(client)
	if err != nil {
		log.Fatalf("Failed to create DMS service: %v", err)
	}
	defer dms.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	switch *action {
	case "all":
		runSIM(ctx, dms)
		runPIN(ctx, dms)
		runUIM(ctx, client)
		runMode(ctx, dms)
		runRevision(ctx, dms)
		runSerial(ctx, dms)
	case "sim":
		runSIM(ctx, dms)
	case "pin":
		runPIN(ctx, dms)
	case "uim":
		runUIM(ctx, client)
	case "mode":
		runMode(ctx, dms)
	case "serial":
		runSerial(ctx, dms)
	case "revision":
		runRevision(ctx, dms)
	case "dump":
		runDump(ctx, client, dms)
	default:
		log.Fatalf("Unknown action: %s", *action)
	}
}

func runSIM(ctx context.Context, dms *qmi.DMSService) {
	fmt.Println("=== DMS SIM Status ===")
	st, err := dms.GetSIMStatus(ctx)
	if err != nil {
		var ns *qmi.NotSupportedError
		if errors.As(err, &ns) {
			fmt.Println("SIM status not supported")
			fmt.Println()
			return
		}
		log.Printf("GetSIMStatus failed: %v", err)
		return
	}
	fmt.Printf("SIM: %s (%d)\n", st.String(), st)
	fmt.Println()
}

func runPIN(ctx context.Context, dms *qmi.DMSService) {
	fmt.Println("=== DMS PIN Status ===")
	p, err := dms.GetPINStatus(ctx)
	if err != nil {
		var ns *qmi.NotSupportedError
		if errors.As(err, &ns) {
			fmt.Println("PIN status not supported")
			fmt.Println()
			return
		}
		log.Printf("GetPINStatus failed: %v", err)
		return
	}
	fmt.Printf("PINStatus: %d\n", p.Status)
	fmt.Printf("VerifyRetriesLeft: %d\n", p.VerifyRetriesLeft)
	fmt.Printf("UnblockRetriesLeft: %d\n", p.UnblockRetriesLeft)
	fmt.Println()
}

func runUIM(ctx context.Context, client *qmi.Client) {
	fmt.Println("=== UIM Card Status ===")
	uim, err := qmi.NewUIMService(client)
	if err != nil {
		log.Printf("NewUIMService failed: %v", err)
		return
	}
	defer uim.Close()

	d, st, err := uim.GetCardStatusDetails(ctx)
	if err != nil {
		log.Printf("GetCardStatusDetails failed: %v", err)
		return
	}
	fmt.Printf("SIM: %s (%d)\n", st.String(), st)
	fmt.Printf("CardState: %d\n", d.CardState)
	fmt.Printf("ErrorCode: %d\n", d.ErrorCode)
	fmt.Printf("NumSlot: %d\n", d.NumSlot)
	fmt.Printf("NumApp: %d\n", d.NumApp)
	fmt.Printf("AppType: %d\n", d.AppType)
	fmt.Printf("AppState: %d\n", d.AppState)
	fmt.Printf("PersoState: %d\n", d.PersoState)
	fmt.Printf("PersoFeature: %d\n", d.PersoFeature)
	fmt.Printf("PersoRetries: %d\n", d.PersoRetries)
	fmt.Printf("PersoUnblockRetries: %d\n", d.PersoUnblockRetries)
	fmt.Printf("AID: %x\n", d.AID)
	fmt.Printf("UsesUPIN: %t\n", d.UsesUPIN)
	fmt.Printf("PIN1State: %d\n", d.PIN1State)
	fmt.Printf("PIN1Retries: %d\n", d.PIN1Retries)
	fmt.Printf("PUK1Retries: %d\n", d.PUK1Retries)
	fmt.Printf("PIN2State: %d\n", d.PIN2State)
	fmt.Printf("PIN2Retries: %d\n", d.PIN2Retries)
	fmt.Printf("PUK2Retries: %d\n", d.PUK2Retries)
	fmt.Printf("UPINState: %d\n", d.UPINState)
	fmt.Printf("UPINRetries: %d\n", d.UPINRetries)
	fmt.Printf("UPUKRetries: %d\n", d.UPUKRetries)
	fmt.Println()
}

func runMode(ctx context.Context, dms *qmi.DMSService) {
	fmt.Println("=== DMS Operating Mode ===")
	m, err := dms.GetOperatingMode(ctx)
	if err != nil {
		log.Printf("GetOperatingMode failed: %v", err)
		return
	}
	fmt.Printf("Mode: %d\n", m)
	fmt.Println()
}

func runRevision(ctx context.Context, dms *qmi.DMSService) {
	fmt.Println("=== DMS Revision ===")
	rev, boot, err := dms.GetDeviceRevision(ctx)
	if err != nil {
		log.Printf("GetDeviceRevision failed: %v", err)
		return
	}
	fmt.Printf("Revision: %q\n", rev)
	fmt.Printf("Boot: %q\n", boot)
	fmt.Println()
}

func runSerial(ctx context.Context, dms *qmi.DMSService) {
	fmt.Println("=== DMS Serial Numbers ===")
	info, err := dms.GetDeviceSerialNumbers(ctx)
	if err != nil {
		log.Printf("GetDeviceSerialNumbers failed: %v", err)
		return
	}
	fmt.Printf("IMEI: %q\n", info.IMEI)
	fmt.Printf("ESN: %q\n", info.ESN)
	fmt.Printf("MEID: %q\n", info.MEID)
	fmt.Println()
}

func runDump(ctx context.Context, client *qmi.Client, dms *qmi.DMSService) {
	fmt.Println("=== DMS Dump TLVs ===")

	type item struct {
		name  string
		msgID uint16
		tlvs  []qmi.TLV
	}

	items := []item{
		{name: "DMSUIMGetState", msgID: qmi.DMSUIMGetState, tlvs: nil},
		{name: "DMSUIMGetPINStatus", msgID: qmi.DMSUIMGetPINStatus, tlvs: nil},
		{name: "DMSGetOperatingMode", msgID: qmi.DMSGetOperatingMode, tlvs: nil},
		{name: "DMSGetDeviceRevID", msgID: qmi.DMSGetDeviceRevID, tlvs: nil},
		{name: "DMSGetDeviceSerialNumbers", msgID: qmi.DMSGetDeviceSerialNumbers, tlvs: nil},
	}

	for _, it := range items {
		resp, err := client.SendRequest(ctx, qmi.ServiceDMS, dms.ClientID(), it.msgID, it.tlvs)
		if err != nil {
			fmt.Printf("%s: request error: %v\n", it.name, err)
			continue
		}
		if err := resp.CheckResult(); err != nil {
			fmt.Printf("%s: result error: %v\n", it.name, err)
			continue
		}
		fmt.Printf("%s: %d TLVs\n", it.name, len(resp.TLVs))
		for _, tlv := range resp.TLVs {
			fmt.Printf("  TLV 0x%02x len=%d val=%x\n", tlv.Type, len(tlv.Value), tlv.Value)
		}
	}
	fmt.Println()
}

func defaultQmiDevice() string {
	candidates := []string{"/dev/cdc-wdm0", "/dev/cdc-wdm1", "/dev/cdc-wdm2", "/dev/cdc-wdm3"}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "/dev/cdc-wdm0"
}
