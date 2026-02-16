package main

/*
#include <stdlib.h>
*/
import "C"
import (
	"io"
	"os"
	"unsafe"

	"iosuite.io/libs/iocore"
)

//export ProcessImage
func ProcessImage(inputPath *C.char, outputPath *C.char) *C.char {
	inPath := C.GoString(inputPath)
	outPath := C.GoString(outputPath)

	iocore.Info("Processing image", "input", inPath, "output", outPath)

	// Mock implementation for skeleton
	inFile, err := os.Open(inPath)
	if err != nil {
		return C.CString(err.Error())
	}
	defer inFile.Close()

	outFile, err := os.Create(outPath)
	if err != nil {
		return C.CString(err.Error())
	}
	defer outFile.Close()

	// In a real implementation, we would use a specific ImageProcessor here.
	// For now, we just copy.
	_, err = io.Copy(outFile, inFile)
	if err != nil {
		return C.CString(err.Error())
	}

	return nil // Success
}

//export FreeString
func FreeString(s *C.char) {
	C.free(unsafe.Pointer(s))
}

func main() {}
