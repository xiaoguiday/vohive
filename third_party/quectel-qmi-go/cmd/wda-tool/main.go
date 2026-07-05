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
	action := flag.String("action", "all", "Action: all, get-format, set-rawip, set-eth, get-qmap, set-qmap, loopback-on, loopback-off, dump")
	qmapInBand := flag.Int("qmap-inband", 0, "QMAP InBandFlowControl for set-qmap (0/1)")
	replicate := flag.Uint("replicate", 0, "Replication factor for loopback-on")
	flag.Parse()

	client, err := qmi.NewClientWithOptions(context.Background(), *devicePath, qmi.ClientOptions{})
	if err != nil {
		log.Fatalf("Failed to create QMI client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wda, err := qmi.NewWDAService(client)
	if err != nil {
		log.Fatalf("Failed to create WDA service: %v", err)
	}
	defer wda.Close()

	switch *action {
	case "all":
		getFormat(ctx, wda)
		getQmap(ctx, wda)
	case "get-format":
		getFormat(ctx, wda)
	case "set-rawip":
		if err := wda.SetDataFormat(ctx, qmi.DataFormat{LinkProtocol: qmi.LinkProtocolIP, UlDataAggregation: 0, DlDataAggregation: 0}); err != nil {
			log.Fatalf("SetDataFormat rawip failed: %v", err)
		}
		getFormat(ctx, wda)
	case "set-eth":
		if err := wda.SetDataFormat(ctx, qmi.DataFormat{LinkProtocol: qmi.LinkProtocolEthernet, UlDataAggregation: 0, DlDataAggregation: 0}); err != nil {
			log.Fatalf("SetDataFormat ethernet failed: %v", err)
		}
		getFormat(ctx, wda)
	case "get-qmap":
		getQmap(ctx, wda)
	case "set-qmap":
		if err := wda.SetQMAPSettings(ctx, qmi.QMAPSettings{InBandFlowControl: uint8(*qmapInBand)}); err != nil {
			log.Fatalf("SetQMAPSettings failed: %v", err)
		}
		getQmap(ctx, wda)
	case "loopback-on":
		if err := wda.SetLoopbackConfig(ctx, qmi.LoopbackConfig{State: 1, ReplicationFactor: uint32(*replicate)}); err != nil {
			log.Printf("SetLoopbackConfig on failed: %v", err)
			return
		}
		fmt.Println("Loopback enabled")
	case "loopback-off":
		if err := wda.SetLoopbackConfig(ctx, qmi.LoopbackConfig{State: 0, ReplicationFactor: 0}); err != nil {
			log.Printf("SetLoopbackConfig off failed: %v", err)
			return
		}
		fmt.Println("Loopback disabled")
	case "dump":
		dump(ctx, client, wda)
	default:
		log.Fatalf("Unknown action: %s", *action)
	}
}

func getFormat(ctx context.Context, wda *qmi.WDAService) {
	fmt.Println("=== WDA Data Format ===")
	f, err := wda.GetDataFormatDetails(ctx)
	if err != nil {
		log.Printf("GetDataFormat failed: %v", err)
		return
	}
	fmt.Printf("QOSSetting: %d\n", f.QOSSetting)
	fmt.Printf("LinkProtocol: %d\n", f.LinkProtocol)
	fmt.Printf("UlDataAggregation: %d\n", f.UlDataAggregation)
	fmt.Printf("DlDataAggregation: %d\n", f.DlDataAggregation)
	fmt.Printf("DlMaxDatagrams: %d\n", f.DlMaxDatagrams)
	fmt.Printf("DlMaxSize: %d\n", f.DlMaxSize)
	fmt.Printf("EndpointType: %d\n", f.EndpointType)
	fmt.Printf("EndpointID: %d\n", f.EndpointID)
	fmt.Println()
}

func getQmap(ctx context.Context, wda *qmi.WDAService) {
	fmt.Println("=== WDA QMAP Settings ===")
	s, err := wda.GetQMAPSettings(ctx)
	if err != nil {
		log.Printf("GetQMAPSettings failed: %v", err)
		return
	}
	fmt.Printf("InBandFlowControl: %d\n", s.InBandFlowControl)
	fmt.Println()
}

func dump(ctx context.Context, client *qmi.Client, wda *qmi.WDAService) {
	fmt.Println("=== WDA Dump TLVs ===")

	type item struct {
		name  string
		msgID uint16
	}

	items := []item{
		{name: "WDAGetDataFormat", msgID: qmi.WDAGetDataFormat},
		{name: "WDAGetQMAPSettings", msgID: qmi.WDAGetQMAPSettings},
	}

	for _, it := range items {
		resp, err := client.SendRequest(ctx, qmi.ServiceWDA, wda.ClientID(), it.msgID, nil)
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
