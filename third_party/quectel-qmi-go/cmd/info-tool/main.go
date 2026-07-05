package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
)

func main() {
	devicePath := flag.String("device", defaultQmiDevice(), "Path to QMI device")
	action := flag.String("action", "all", "Action: all, device, sim")
	flag.Parse()

	// Initialize client
	client, err := qmi.NewClientWithOptions(context.Background(), *devicePath, qmi.ClientOptions{})
	if err != nil {
		log.Fatalf("Failed to create QMI client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if *action == "all" || *action == "device" {
		testDeviceInfo(ctx, client)
	}

	if *action == "all" || *action == "sim" {
		testSIMInfo(ctx, client)
	}
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

func testDeviceInfo(ctx context.Context, client *qmi.Client) {
	fmt.Println("=== Testing Device Info (DMS) ===")
	dms, err := qmi.NewDMSService(client)
	if err != nil {
		log.Printf("Failed to create DMS service: %v", err)
		return
	}
	defer dms.Close()

	// Get Device Serial Numbers (IMEI)
	fmt.Println("Getting Device Serial Numbers...")
	ids, err := dms.GetDeviceSerialNumbers(ctx)
	if err != nil {
		log.Printf("Error getting serial numbers: %v", err)
	} else {
		fmt.Printf("IMEI: %s\n", ids.IMEI)
		// IMSI/ICCID are typically not in GetDeviceSerialNumbers (0x0025) which is for ME info.
		// They are usually in UIM or NAS/DMS specialized calls.
		// We will test them in testSIMInfo below.
	}

	// Get Operating Mode
	fmt.Println("Getting Operating Mode...")
	mode, err := dms.GetOperatingMode(ctx)
	if err != nil {
		log.Printf("Error getting operating mode: %v", err)
	} else {
		fmt.Printf("Operating Mode: %d\n", mode)
	}
	fmt.Println()
}

func testSIMInfo(ctx context.Context, client *qmi.Client) {
	fmt.Println("=== Testing SIM Info (UIM) ===")
	uim, err := qmi.NewUIMService(client)
	if err != nil {
		log.Printf("Failed to create UIM service: %v", err)
		return
	}
	defer uim.Close()

	// Read ICCID via Transparent File (0x2FE2)
	fmt.Println("Reading ICCID via UIM (File 0x2FE2)...")

	iccid, err := uim.GetICCID(ctx)
	if err != nil {
		log.Printf("ICCID read failed: %v", err)
	} else {
		fmt.Printf("ICCID: %s\n", iccid)
	}

	// Read IMSI via Transparent File (0x6F07)
	fmt.Println("Reading IMSI via UIM (File 0x6F07)...")

	imsi, err := uim.GetIMSI(ctx)
	if err != nil {
		log.Printf("IMSI read failed: %v", err)
	} else {
		fmt.Printf("IMSI: %s\n", imsi)
	}
	fmt.Println()
}

func swapNibbles(data []byte) string {
	res := ""
	for _, b := range data {
		low := b & 0x0F
		high := (b >> 4) & 0x0F
		if low < 10 {
			res += fmt.Sprintf("%d", low)
		}
		if high < 10 {
			res += fmt.Sprintf("%d", high)
		}
	}
	return res
}
