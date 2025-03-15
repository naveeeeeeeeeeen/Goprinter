package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"net/http"

	"github.com/google/gousb"
	"github.com/nfnt/resize"
)

func enableCORS(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		// Handle Preflight (OPTIONS request)
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		handler(w, r)
	}
}

func ConvertToESCPos(img image.Image) []byte {
	const (
		// printableWidth  = 384 // 2 inches (48mm) at 203 DPI
		// printableHeight = 192 // 1 inch (24mm) at 203 DPI
		printableWidth  = 384 // 2 inches (48mm) at 203 DPI
		printableHeight = 192 // 1 inch (24mm) at 203 DPI
	)

	// Resize image to exact label size to prevent cutoff & wrapping
	img = resize.Resize(printableWidth, printableHeight, img, resize.Bicubic)

	bounds := img.Bounds()
	width := bounds.Dx() + 125
	height := bounds.Dy()

	// Calculate width in bytes (ESC/POS requires width to be in 8-pixel chunks)
	widthBytes := width / 8
	if width%8 != 0 {
		widthBytes++ // Ensure it’s always byte-aligned
	}

	var escposData []byte
	escposData = append(escposData, 0x1B, 0x40) // ESC @ (Initialize printer)

	// **Set Raster Bit Image Mode**
	escposData = append(escposData, 0x1D, 0x76, 0x30, 0x00)                // GS v 0 (Raster Bit Image Mode)
	escposData = append(escposData, byte(widthBytes), byte(widthBytes>>8)) // Width in bytes
	escposData = append(escposData, byte(height), byte(height>>8))         // Height in pixels

	// Convert Image to ESC/POS Printable Format
	for y := 0; y < height; y++ {
		for xByte := 0; xByte < widthBytes; xByte++ {
			var byteVal byte
			for bit := 0; bit < 8; bit++ {
				x := (xByte * 8) + bit
				if x < width {
					gray := color.GrayModel.Convert(img.At(x, y)).(color.Gray)
					if gray.Y < 128 { // Threshold for black/white
						byteVal |= (1 << (7 - bit))
					}
				}
			}
			escposData = append(escposData, byteVal)
		}
	}

	// Final line feed (ensures clean print)
	escposData = append(escposData, 0x0A, 0x0A)

	return escposData
}

func printBarcode(w http.ResponseWriter, r *http.Request) {
	ctx := gousb.NewContext()
	defer ctx.Close()

	imageBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read image data", http.StatusBadRequest)
		return
	}

	defer r.Body.Close()

	img, err := png.Decode(bytes.NewReader(imageBytes))

	if err != nil {
		fmt.Println(err)
		http.Error(w, "Invalid image format", http.StatusBadRequest)
		return
	}

	// resizedImage := resize.Resize(384, 0, img, resize.Lanczos3)
	rawBytes := ConvertToESCPos(img)
	// fmt.Print(rawBytes)

	vendorId := gousb.ID(0x4b43)
	productId := gousb.ID(0x3538)

	dev, err := ctx.OpenDeviceWithVIDPID(vendorId, productId)

	if err != nil {
		log.Fatal("error connecting to printer")
	}

	defer dev.Close()

	fmt.Println("printer connected")

	intf, done, err := dev.DefaultInterface()
	if err != nil {
		log.Fatal("failed to claim interface")
	}

	defer done()

	outEndpoint, err := intf.OutEndpoint(1)
	if err != nil {
		log.Fatal("error opening writer")
	}

	initCmd := []byte{0x1B, 0x40}
	_, _ = outEndpoint.Write(initCmd)
	_, _ = outEndpoint.Write(rawBytes)
	// _, _ = outEndpoint.Write([]byte{0x0A})

	// if err != nil {
	// 	log.Fatalf("error writing to printer %v", err)
	// }

	// fmt.Println("printing status ", status)
}

func main() {
	http.HandleFunc("/print", enableCORS(printBarcode))
	fmt.Println("print agent running on port 9000")
	http.ListenAndServe(":9000", nil)
}
