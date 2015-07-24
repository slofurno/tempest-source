package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"runtime"
	"sync"
	"time"
)

type Tempest struct {
	Name   string `json:"name"`
	Base   string `json:"base"`
	Suffix string `json:"suffix"`
}

var tempestCache map[string]*Tempest

var hub *UserHub
var tempLock sync.Mutex

func init() {
	hub = &UserHub{}
	tempestCache = map[string]*Tempest{}
}

func main() {

	runtime.GOMAXPROCS(runtime.NumCPU())

	timer := time.NewTicker(time.Minute * 1)

	go func() {
		for {

			func() {

				res, err := http.Get("http://poetempest.com/api/v0/current_tempests")
				if err != nil {
					fmt.Println(err.Error())
					// handle error
				}
				defer res.Body.Close()
				body, err := ioutil.ReadAll(res.Body)

				fmt.Println("processing update")

				currentTempests := map[string]*Tempest{}
				update := map[string]*Tempest{}
				updatedMaps := 0

				err = json.Unmarshal(body, &currentTempests)

				if err != nil {
					fmt.Println(err.Error())
					return
				}

				func() {
					tempLock.Lock()
					defer tempLock.Unlock()

					for key, currentTempest := range currentTempests {

						previousTempest, ok := tempestCache[key]

						if !ok || previousTempest.Name != currentTempest.Name {
							if ok {
								fmt.Println(key, "changed from", previousTempest.Name, "to", currentTempest.Name)

							}
							tempestCache[key] = currentTempest
							update[key] = currentTempest
							updatedMaps++
						}

					}
				}()

				if updatedMaps > 0 {
					up := &UpdatePacket{}
					j, err := json.Marshal(update)
					if err != nil {
						return
					}

					up.Event = "TEMPEST"
					up.Message = string(j)

					hub.Broadcast(up)
				}

				fmt.Println(updatedMaps, "updates")
			}()

			<-timer.C

		}
	}()

	http.HandleFunc("/es", eventSource)
	http.ListenAndServe(":5678", nil)

}

func eventSource(w http.ResponseWriter, req *http.Request) {

	h, _ := w.(http.Hijacker)
	conn, rw, _ := h.Hijack()
	defer conn.Close()

	rw.Write([]byte("HTTP/1.1 200 OK\r\n"))
	rw.Write([]byte("Access-Control-Allow-Origin: *\r\n"))
	rw.Write([]byte("Content-Type: text/event-stream\r\n\r\n"))
	rw.Flush()

	disconnect := make(chan bool, 1)

	go func() {
		_, err := rw.ReadByte()
		if err == io.EOF {
			disconnect <- true
		}
	}()

	uc := newUserConnection()
	hub.Add(uc)

	func() {
		tempLock.Lock()
		defer tempLock.Unlock()

		tj, err := json.Marshal(tempestCache)

		if err != nil {
			return
		}

		rw.Write([]byte("event: " + "TEMPEST" + "\r\n"))
		rw.Write([]byte("data: " + string(tj) + "\r\n\r\n"))

	}()

	for {

		select {
		case <-disconnect:
			return

		case update := <-uc.Queue:

			rw.Write([]byte("event: " + update.Event + "\r\n"))
			rw.Write([]byte("data: " + update.Message + "\r\n\r\n"))
			rw.Flush()

		}

	}

}
