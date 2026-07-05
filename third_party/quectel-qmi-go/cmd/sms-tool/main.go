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
	action := flag.String("action", "list", "Action to perform: list, read, read-meta, delete, delete-tag, modify-tag, mark-read, smsc")
	index := flag.Int("index", -1, "Message index for read/delete/modify-tag")
	storage := flag.Int("storage", 1, "Storage type (0=UIM, 1=NV)")
	tag := flag.Int("tag", 1, "Message tag for delete-tag/modify-tag (0=Read, 1=NotRead, 2=Sent, 3=NotSent)")
	allTags := flag.Bool("all-tags", false, "For list action: query all tags (noisy on some modems)")
	flag.Parse()

	// Initialize client
	client, err := qmi.NewClientWithOptions(context.Background(), *devicePath, qmi.ClientOptions{})
	if err != nil {
		log.Fatalf("Failed to create QMI client: %v", err)
	}
	defer client.Close()

	// Initialize WMS service
	wms, err := qmi.NewWMSService(client)
	if err != nil {
		log.Fatalf("Failed to create WMS service: %v", err)
	}
	defer wms.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	switch *action {
	case "list":
		fmt.Println("Listing messages...")
		if *allTags {
			tags := []qmi.MessageTagType{
				qmi.TagTypeMTNotRead,
				qmi.TagTypeMTRead,
				qmi.TagTypeMOSent,
				qmi.TagTypeMONotSent,
			}

			for _, tag := range tags {
				msgs, err := wms.ListMessages(ctx, uint8(*storage), tag)
				if err != nil {
					log.Printf("Error listing messages for tag %v: %v", tag, err)
					continue
				}
				for _, msg := range msgs {
					fmt.Printf("Index: %d, Tag: %v\n", msg.Index, msg.Tag)
				}
			}
			break
		}

		msgs, err := wms.ListMessagesAuto(ctx, uint8(*storage))
		if err != nil {
			log.Fatalf("Failed to list messages: %v", err)
		}
		for _, msg := range msgs {
			fmt.Printf("Index: %d, Tag: %v\n", msg.Index, msg.Tag)
		}

	case "read":
		if *index < 0 {
			log.Fatal("Please provide -index for read action")
		}
		fmt.Printf("Reading message at index %d...\n", *index)
		data, err := wms.RawReadMessage(ctx, uint8(*storage), uint32(*index))
		if err != nil {
			log.Fatalf("Failed to read message: %v", err)
		}
		fmt.Printf("Raw PDU: %x\n", data)
		fmt.Printf("ASCII (Approx): %s\n", string(data))

	case "read-meta":
		if *index < 0 {
			log.Fatal("Please provide -index for read-meta action")
		}
		fmt.Printf("Reading message meta at index %d...\n", *index)
		tagVal, ok, data, err := wms.RawReadMessageMeta(ctx, uint8(*storage), uint32(*index))
		if err != nil {
			log.Fatalf("Failed to read message: %v", err)
		}
		if ok {
			fmt.Printf("Tag: %d\n", tagVal)
		} else {
			fmt.Printf("Tag: (not present in response)\n")
		}
		fmt.Printf("Raw PDU: %x\n", data)

	case "delete":
		if *index < 0 {
			log.Fatal("Please provide -index for delete action")
		}
		fmt.Printf("Deleting message at index %d (storage: %d)...\n", *index, *storage)
		err := wms.DeleteMessage(ctx, uint8(*storage), uint32(*index))
		if err != nil {
			log.Fatalf("Failed to delete message: %v", err)
		}
		fmt.Println("Message deleted successfully")

	case "delete-tag":
		fmt.Printf("Deleting messages by tag %d (storage: %d)...\n", *tag, *storage)
		err := wms.DeleteMessagesByTag(ctx, uint8(*storage), qmi.MessageTagType(*tag), qmi.MessageModeGW)
		if err != nil {
			log.Fatalf("Failed to delete messages by tag: %v", err)
		}
		fmt.Println("Messages deleted successfully")

	case "modify-tag":
		if *index < 0 {
			log.Fatal("Please provide -index for modify-tag action")
		}
		beforeTag, beforeOk, _, _ := wms.RawReadMessageMeta(ctx, uint8(*storage), uint32(*index))
		if beforeOk {
			fmt.Printf("Before Tag: %d\n", beforeTag)
		}
		fmt.Printf("Modifying message tag at index %d to %d (storage: %d)...\n", *index, *tag, *storage)
		if err := wms.ModifyMessageTag(ctx, uint8(*storage), uint32(*index), qmi.MessageTagType(*tag)); err != nil {
			log.Fatalf("Failed to modify message tag: %v", err)
		}
		afterTag, afterOk, _, _ := wms.RawReadMessageMeta(ctx, uint8(*storage), uint32(*index))
		if afterOk {
			fmt.Printf("After Tag: %d\n", afterTag)
		}
		fmt.Println("Message tag modified successfully")

	case "mark-read":
		if *index < 0 {
			log.Fatal("Please provide -index for mark-read action")
		}
		beforeTag, beforeOk, _, _ := wms.RawReadMessageMeta(ctx, uint8(*storage), uint32(*index))
		if beforeOk {
			fmt.Printf("Before Tag: %d\n", beforeTag)
		}
		fmt.Printf("Marking message as read at index %d (storage: %d)...\n", *index, *storage)
		if err := wms.ModifyMessageTag(ctx, uint8(*storage), uint32(*index), qmi.TagTypeMTRead); err != nil {
			log.Fatalf("Failed to mark message as read: %v", err)
		}
		afterTag, afterOk, _, _ := wms.RawReadMessageMeta(ctx, uint8(*storage), uint32(*index))
		if afterOk {
			fmt.Printf("After Tag: %d\n", afterTag)
		}
		fmt.Println("Message marked as read successfully")

	case "smsc":
		fmt.Println("Getting SMSC Address...")
		addr, err := wms.GetSMSCAddress(ctx)
		if err != nil {
			log.Fatalf("Failed to get SMSC address: %v", err)
		}
		fmt.Printf("SMSC Address: %s\n", addr)

	default:
		log.Fatalf("Unknown action: %s", *action)
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
