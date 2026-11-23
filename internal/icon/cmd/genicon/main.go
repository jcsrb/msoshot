// genicon writes the app icon as a PNG, for embedding into the Windows exe.
package main

import (
	"os"

	"outlook-render/internal/icon"
)

func main() {
	if len(os.Args) != 2 {
		os.Stderr.WriteString("usage: genicon <out.png>\n")
		os.Exit(2)
	}
	if err := os.WriteFile(os.Args[1], icon.PNG(256), 0o644); err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}
