// main.go
package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend
var assets embed.FS

var bufferSize int = 256
var otoCtx, _ = initOto()
var loggerList map[string]*os.File = make(map[string]*os.File)

var filterList = getTwitchConfigFromFile("config.txt").FilterList

func containsAny(text string, keywords []string) bool {
	textLower := strings.ToLower(text)
	for _, keyword := range keywords {
		if strings.Contains(textLower, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func main() {
	os.Mkdir("logs", 0700)
	log.Println(filterList)

	t := time.Now()
	formatted := fmt.Sprintf("%d-%02d-%02d",
		t.Year(), t.Month(), t.Day())

	f, err := os.OpenFile(filepath.Join("logs", formatted+"_log.txt"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	defer f.Close()

	// Check if we're running with a console
	// if isConsoleAvailable() {
	// 	mw := io.MultiWriter(os.Stdout, f)
	// 	log.SetOutput(mw)
	// } else {
	// 	log.SetOutput(f)
	// }
	log.SetOutput(f)
	go func() {
		if err := Fetch7TVGlobalEmotes(); err != nil {
			log.Printf("failed to fetch 7TV global emotes: %v", err)
		}
		if err := FetchBTTVGlobalEmotes(); err != nil {
			log.Printf("failed to fetch BTTV global emotes: %v", err)
		}
		if err := FetchFFZGlobalEmotes(); err != nil {
			log.Printf("failed to fetch FFZ global emotes: %v", err)
		}
	}()

	app := NewApp()

	err = wails.Run(&options.App{
		Title:  "Twitch Chat",
		Width:  785,
		Height: 768,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 26, G: 26, B: 26, A: 1},
		OnStartup:        app.OnStartup,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
