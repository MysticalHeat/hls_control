package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/gin-gonic/gin"
	ffmpeg "github.com/u2takey/ffmpeg-go"
)

func startStream(port int, index int) {
    for {
        err := ffmpeg.Input(fmt.Sprintf("udp://localhost:%d", port)).
        Output(fmt.Sprintf("./streams/stream%d.m3u8", index), ffmpeg.KwArgs{
            "c": "copy", 
            "f": "hls", 
            "hls_time": 4, 
            "hls_list_size": 4, 
            "hls_flags": "delete_segments+append_list+program_date_time",
            "hls_segment_filename": fmt.Sprintf("./streams/stream_%d", index) + "_%03d.ts",
        }).
        OverWriteOutput().ErrorToStdOut().Run()

        log.Printf("Stream ended port %d index %d", port, index)
        if err != nil {
            log.Print(err)
        }
    }
    
}

func main() {

    file, err := os.OpenFile("error.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        log.Fatal(err)
    }
    defer file.Close()

    log.SetOutput(file)

    _, err = os.Stat("streams")

    if os.IsNotExist(err) {
        err = os.Mkdir("streams", 0755)
            if err != nil {
                log.Fatal("Не удалось создать директорию:", err)
            }
    }

    var servePort = flag.Int("p", 3002, "Port to serve HLS streams")
    var startPort = flag.Int("s", 2220, "Starting port for UDP streams")
    var streamCount = flag.Int("n", 1, "Number of streams to handle")

    flag.Parse()

    for i := 0; i < *streamCount; i++ {
        go startStream(*startPort + i, i)
    }

    r := gin.Default()

    r.Static("/streams", "./streams")

    err = r.Run(fmt.Sprintf(":%d", *servePort))
    log.Fatal(err)

}