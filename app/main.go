package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	ffmpeg "github.com/u2takey/ffmpeg-go"
)

type StreamEvent struct {
    channelId int `json:"channelId"`
    eventType string `json:"eventType"`
}

type Client struct {
    send chan StreamEvent
}

type EventManager struct {
    clients map[*Client]bool
    mu sync.RWMutex
}

func NewEventManager() *EventManager {
    return &EventManager{
        clients: make(map[*Client]bool),
    }
}

func (em *EventManager) AddClient(client *Client) {
    em.mu.Lock()
    defer em.mu.Unlock()
    em.clients[client] = true
}

func (em *EventManager) RemoveClient(client *Client) {
    em.mu.Lock()
    defer em.mu.Unlock()
    delete(em.clients,client)
}

func (em *EventManager) Broadcast(event StreamEvent) {
    em.mu.RLock()
    defer em.mu.RUnlock()

    for client := range em.clients {
        select {
        case client.send <- event:
        }
    }
}

func (em *EventManager) SSEHandler (c *gin.Context) {
    c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	
	client := &Client{
		send: make(chan StreamEvent, 10), 
	}

    c.SSEvent("", "")
    c.Writer.Flush()

	em.AddClient(client)
	defer em.RemoveClient(client)

	go func() {
		for event := range client.send {
			c.SSEvent(event.eventType, event.channelId)
			c.Writer.Flush()
		}
	}()

	<-c.Done()
	close(client.send)
}


func startStream(ip string, port int, index int, em *EventManager) {
    file, err := os.OpenFile(fmt.Sprintf("logs/channel_%d.log", index), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
    if err != nil {
        log.Fatal("Failed to open log file", err)
    }

    multiWriter := io.MultiWriter(os.Stdout, file)
   

    for {
        err := ffmpeg.Input(fmt.Sprintf("udp://%s:%d?timeout=60000000", ip, port)).
            Output(fmt.Sprintf("./streams/stream%d.m3u8", index), ffmpeg.KwArgs{
                "c": "copy", 
                "f": "hls", 
                "hls_time": 4, 
                "hls_list_size": 4, 
                "hls_flags": "delete_segments+append_list+program_date_time",
                "hls_segment_filename": fmt.Sprintf("./streams/stream_%d", index) + "_%03d.ts",
            }).
            OverWriteOutput().WithErrorOutput(multiWriter).Run()

        log.Printf("Stream ended port %d index %d", port, index)
        if err != nil {
            log.Print(err)
        }

        em.Broadcast(StreamEvent{channelId: index, eventType: "closed"})

        walkErr := filepath.Walk("./streams", func(path string, info os.FileInfo, err error) error {
            if err != nil {
                return err
            }

            if strings.HasPrefix(info.Name(), fmt.Sprintf("stream_%d", index)) {
                removeErr := os.Remove(path)
                if removeErr != nil {
                    return removeErr
                }
            }
            return nil
        })

        os.Remove(fmt.Sprintf("./streams/stream%d.m3u8", index))


        if walkErr != nil {
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

    _, err = os.Stat("logs")

    if os.IsNotExist(err) {
        err = os.Mkdir("logs", 0755)
            if err != nil {
                log.Fatal("Не удалось создать директорию:", err)
            }
    }

    var servePort = flag.Int("p", 3002, "Port to serve HLS streams")
    var startPort = flag.Int("s", 2220, "Starting port for UDP streams")
    var streamCount = flag.Int("n", 1, "Number of streams to handle")
    var udpIp = flag.String("i", "localhost", "Udp ip")

    flag.Parse()

	em := NewEventManager()

    for i := 0; i < *streamCount; i++ {
        go startStream(*udpIp, *startPort + i, i, em)
    }

    r := gin.Default()

    r.Static("/streams", "./streams")

    r.GET("/events", em.SSEHandler)

    err = r.Run(fmt.Sprintf(":%d", *servePort))
    log.Fatal(err)

}