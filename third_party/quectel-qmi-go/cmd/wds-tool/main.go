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
	action := flag.String("action", "all", "Action: all, status, runtime4, runtime6, profiles, profile, dump")
	profileType := flag.Int("profile-type", 0, "Profile type (0=3GPP, 1=3GPP2)")
	profileIndex := flag.Int("profile-index", 1, "Profile index for profile action")
	flag.Parse()

	client, err := qmi.NewClientWithOptions(context.Background(), *devicePath, qmi.ClientOptions{})
	if err != nil {
		log.Fatalf("Failed to create QMI client: %v", err)
	}
	defer client.Close()

	wds, err := qmi.NewWDSService(client)
	if err != nil {
		log.Fatalf("Failed to create WDS service: %v", err)
	}
	defer wds.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	switch *action {
	case "all":
		runStatus(ctx, wds)
		runRuntime(ctx, wds, 0x04)
		runProfiles(ctx, wds, uint8(*profileType))
	case "status":
		runStatus(ctx, wds)
	case "runtime4":
		runRuntime(ctx, wds, 0x04)
	case "runtime6":
		runRuntime(ctx, wds, 0x06)
	case "profiles":
		runProfiles(ctx, wds, uint8(*profileType))
	case "profile":
		runProfile(ctx, wds, uint8(*profileType), uint8(*profileIndex))
	case "dump":
		runDump(ctx, client, wds)
	default:
		log.Fatalf("Unknown action: %s", *action)
	}
}

func runStatus(ctx context.Context, wds *qmi.WDSService) {
	fmt.Println("=== WDS Packet Service Status ===")
	st, err := wds.GetPacketServiceStatus(ctx)
	if err != nil {
		log.Printf("GetPacketServiceStatus failed: %v", err)
		return
	}
	fmt.Printf("Status: %s (%d)\n", st.String(), st)
	fmt.Println()
}

func runRuntime(ctx context.Context, wds *qmi.WDSService, ipFamily uint8) {
	fmt.Printf("=== WDS Runtime Settings (ipFamily=0x%02x) ===\n", ipFamily)
	rs, err := wds.GetRuntimeSettings(ctx, ipFamily)
	if err != nil {
		var outOfCall *qmi.OutOfCallError
		if errors.As(err, &outOfCall) {
			fmt.Println("Not connected (out of call)")
			fmt.Println()
			return
		}
		log.Printf("GetRuntimeSettings failed: %v", err)
		return
	}
	fmt.Printf("IPv4Address: %v\n", rs.IPv4Address)
	fmt.Printf("IPv4Subnet: %v\n", rs.IPv4Subnet)
	fmt.Printf("IPv4Gateway: %v\n", rs.IPv4Gateway)
	fmt.Printf("IPv4DNS1: %v\n", rs.IPv4DNS1)
	fmt.Printf("IPv4DNS2: %v\n", rs.IPv4DNS2)
	fmt.Printf("IPv6Address: %v/%d\n", rs.IPv6Address, rs.IPv6Prefix)
	fmt.Printf("IPv6Gateway: %v\n", rs.IPv6Gateway)
	fmt.Printf("IPv6DNS1: %v\n", rs.IPv6DNS1)
	fmt.Printf("IPv6DNS2: %v\n", rs.IPv6DNS2)
	fmt.Printf("MTU: %d\n", rs.MTU)
	fmt.Println()
}

func runProfiles(ctx context.Context, wds *qmi.WDSService, profileType uint8) {
	fmt.Println("=== WDS Profile List ===")
	ps, err := wds.GetProfileList(ctx, profileType)
	if err != nil {
		log.Printf("GetProfileList failed: %v", err)
		return
	}
	for _, p := range ps {
		fmt.Printf("Type=%d Index=%d Name=%q\n", p.Type, p.Index, p.Name)
	}
	fmt.Println()
}

func runProfile(ctx context.Context, wds *qmi.WDSService, profileType, profileIndex uint8) {
	fmt.Println("=== WDS Profile Settings ===")
	apn, err := wds.GetProfileSettings(ctx, profileType, profileIndex)
	if err != nil {
		log.Printf("GetProfileSettings failed: %v", err)
		return
	}
	fmt.Printf("Type=%d Index=%d APN=%q\n", profileType, profileIndex, apn)
	fmt.Println()
}

func runDump(ctx context.Context, client *qmi.Client, wds *qmi.WDSService) {
	fmt.Println("=== WDS Dump TLVs ===")

	type item struct {
		name  string
		msgID uint16
		tlvs  []qmi.TLV
	}

	items := []item{
		{name: "WDSGetPktSrvcStatus", msgID: qmi.WDSGetPktSrvcStatus, tlvs: nil},
		{name: "WDSGetRuntimeSettings", msgID: qmi.WDSGetRuntimeSettings, tlvs: []qmi.TLV{qmi.NewTLVUint32(0x10, qmi.RuntimeMaskIPAddr|qmi.RuntimeMaskGateway|qmi.RuntimeMaskDNS|qmi.RuntimeMaskMTU)}},
		{name: "WDSGetProfileList(no TLV)", msgID: qmi.WDSGetProfileList, tlvs: nil},
		{name: "WDSGetProfileList(3GPP, TLV0x01)", msgID: qmi.WDSGetProfileList, tlvs: []qmi.TLV{qmi.NewTLVUint8(0x01, 0)}},
		{name: "WDSGetProfileList(3GPP, TLV0x11)", msgID: qmi.WDSGetProfileList, tlvs: []qmi.TLV{qmi.NewTLVUint8(0x11, 0)}},
	}

	for _, it := range items {
		resp, err := client.SendRequest(ctx, qmi.ServiceWDS, wds.ClientID(), it.msgID, it.tlvs)
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
