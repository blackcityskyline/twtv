//go:build ignore

// Запусти: go run avatar_debug.go
// Покажет какой протокол определён, выведет сырой вывод chafa для sixel,
// и нарисует тестовый аватар прямо в терминале.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func main() {
	term := os.Getenv("TERM")
	termProg := os.Getenv("TERM_PROGRAM")
	fmt.Printf("TERM=%q  TERM_PROGRAM=%q\n", term, termProg)

	_, chafaErr := exec.LookPath("chafa")
	fmt.Printf("chafa in PATH: %v\n", chafaErr == nil)

	// Minimal 1×1 white PNG
	png1x1 := []byte{
		0x89,0x50,0x4e,0x47,0x0d,0x0a,0x1a,0x0a,0x00,0x00,0x00,0x0d,0x49,0x48,0x44,0x52,
		0x00,0x00,0x00,0x01,0x00,0x00,0x00,0x01,0x08,0x02,0x00,0x00,0x00,0x90,0x77,0x53,
		0xde,0x00,0x00,0x00,0x0c,0x49,0x44,0x41,0x54,0x08,0xd7,0x63,0xf8,0xcf,0xc0,0x00,
		0x00,0x00,0x02,0x00,0x01,0xe2,0x21,0xbc,0x33,0x00,0x00,0x00,0x00,0x49,0x45,0x4e,
		0x44,0xae,0x42,0x60,0x82,
	}
	tmp, _ := os.CreateTemp("", "probe-*.png")
	tmp.Write(png1x1)
	tmp.Close()
	defer os.Remove(tmp.Name())

	out, err := exec.Command("chafa","--format","sixels","--size","1x1",tmp.Name()).Output()
	fmt.Printf("chafa probe err=%v  output len=%d\n", err, len(out))
	fmt.Printf("contains 'Pq': %v  contains ESC-P: %v\n",
		strings.Contains(string(out), "Pq"),
		strings.Contains(string(out), "\x1bP"))

	// Now try a real 8x4 render
	out2, err2 := exec.Command("chafa","--format","sixels","--size","8x4","--stretch",tmp.Name()).Output()
	fmt.Printf("8x4 render err=%v  output len=%d\n", err2, len(out2))

	// Try with --font-ratio
	out3, err3 := exec.Command("chafa","--format","sixels","--size","8x4","--stretch","--font-ratio","1/2",tmp.Name()).Output()
	fmt.Printf("8x4 +font-ratio render err=%v  output len=%d\n", err3, len(out3))

	// Print the raw sixel to terminal
	if len(out3) > 0 {
		fmt.Println("--- writing sixel to terminal ---")
		os.Stdout.Write(out3)
		fmt.Println()
		fmt.Println("--- end ---")
	}
}
