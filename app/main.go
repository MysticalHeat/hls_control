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
    log.Printf("[SSE] Client connected. Total clients: %d", len(em.clients))
}

func (em *EventManager) RemoveClient(client *Client) {
    em.mu.Lock()
    defer em.mu.Unlock()
    delete(em.clients,client)
    log.Printf("[SSE] Client disconnected. Total clients: %d", len(em.clients))
}

func (em *EventManager) Broadcast(event StreamEvent) {
    em.mu.RLock()
    defer em.mu.RUnlock()

    log.Printf("[SSE] Broadcasting event: type=%s, channelId=%d to %d clients", event.eventType, event.channelId, len(em.clients))
    
    for client := range em.clients {
        select {
        case client.send <- event:
            log.Printf("[SSE] Event sent to client successfully")
        default:
            log.Printf("[SSE] Failed to send event to client (channel full)")
        }
    }
}

func (em *EventManager) SSEHandler (c *gin.Context) {
    c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	log.Printf("[SSE] New client connecting from %s", c.ClientIP())
	
	client := &Client{
		send: make(chan StreamEvent, 10), 
	}

	em.AddClient(client)
	
	// Отправляем начальный комментарий для установки соединения
	c.Writer.Write([]byte(": connected\n\n"))
	c.Writer.Flush()

	// Канал для сигнала о закрытии
	done := make(chan bool)

	go func() {
		log.Printf("[SSE] Started event sender goroutine for client %s", c.ClientIP())
		for {
			select {
			case event, ok := <-client.send:
				if !ok {
					log.Printf("[SSE] Client channel closed for %s", c.ClientIP())
					return
				}
				log.Printf("[SSE] Sending event to client %s: type=%s, channelId=%d", c.ClientIP(), event.eventType, event.channelId)
				c.SSEvent(event.eventType, event.channelId)
				c.Writer.Flush()
			case <-done:
				log.Printf("[SSE] Done signal received for %s", c.ClientIP())
				return
			}
		}
	}()

	// Ждем закрытия соединения
	<-c.Request.Context().Done()
	log.Printf("[SSE] Client %s disconnected", c.ClientIP())
	
	// Сигнализируем горутине о завершении
	close(done)
	
	// Удаляем клиента и закрываем канал
	em.RemoveClient(client)
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
                // Video - copy without re-encoding
                "c:v": "copy",
                
                // Audio re-encoding with sync for gap prevention
                "c:a": "aac",
                "b:a": "128k",
                "ar": "44100",
                "ac": "2",
                "af": "aresample=async=1:min_hard_comp=0.100000:first_pts=0",
                
                // HLS configuration with fMP4 for Chrome stability
                "f": "hls", 
                "hls_time": "4", 
                "hls_list_size": "10", 
                "hls_flags": "independent_segments+discont_start+split_by_time+delete_segments+append_list+program_date_time",
                "hls_segment_type": "fmp4",
                "hls_segment_filename": fmt.Sprintf("./streams/stream_%d", index) + "_%03d.m4s",
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

            if strings.HasPrefix(info.Name(), fmt.Sprintf("stream_%d", index)) && 
               (strings.HasSuffix(info.Name(), ".m4s") || strings.HasSuffix(info.Name(), ".ts")) {
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