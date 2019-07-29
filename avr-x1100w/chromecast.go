package main

import (
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/micro/mdns"
	"github.com/stapelberg/zkj-nas-tools/avr-x1100w/cast_channel"
)

type chromecastDevice int

const (
	chromecast = iota
	chromecastAudio
)

const castService = "_googlecast._tcp"

type payloadHeaders struct {
	Type      string `json:"type"`
	RequestId *int   `json:"requestId,omitempty"`
}

func read(conn net.Conn) (cast_channel.CastMessage, error) {
	var message cast_channel.CastMessage
	var length uint32

	if err := binary.Read(conn, binary.BigEndian, &length); err != nil {
		return message, err
	}
	log.Printf("reading %d bytes from chromecast\n", length)
	packet := make([]byte, length)
	n, err := conn.Read(packet)
	if err != nil {
		return message, err
	}
	if n != int(length) {
		return message, fmt.Errorf("Short read. want %d bytes, got %d bytes", length, n)
	}
	if err := proto.Unmarshal(packet, &message); err != nil {
		return message, fmt.Errorf("Error unmarshaling proto: %v", err)
	}
	return message, nil
}

func send(conn net.Conn, headers payloadHeaders, msg *cast_channel.CastMessage) error {
	headersJson, err := json.Marshal(&headers)
	if err != nil {
		return fmt.Errorf("Error marshaling JSON: %v", err)
	}
	msg.ProtocolVersion = cast_channel.CastMessage_CASTV2_1_0.Enum()
	msg.PayloadType = cast_channel.CastMessage_STRING.Enum()
	msg.PayloadUtf8 = proto.String(string(headersJson))

	b, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("Error marshaling proto: %v", err)
	}
	if err := binary.Write(conn, binary.BigEndian, uint32(len(b))); err != nil {
		return err
	}
	n, err := conn.Write(b)
	if err != nil {
		return err
	}
	// As per io/ioutil.WriteFile
	if err == nil && n < len(b) {
		return io.ErrShortWrite
	}
	return nil
}

func pollChromecast(deviceType chromecastDevice, done chan bool, hostport string) error {
	var requestId int

	// TODO: use setreadtimeout?
	conn, err := tls.Dial("tcp", hostport, &tls.Config{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return err
	}
	defer conn.Close()

	// Connect to the receiver and request status to get the state machine going.
	requestId++
	if err := send(conn, payloadHeaders{Type: "CONNECT", RequestId: &requestId}, &cast_channel.CastMessage{
		SourceId:      proto.String("sender-0"),
		DestinationId: proto.String("receiver-0"),
		Namespace:     proto.String("urn:x-cast:com.google.cast.tp.connection"),
	}); err != nil {
		return err
	}

	requestId++
	if err := send(conn, payloadHeaders{Type: "GET_STATUS", RequestId: &requestId}, &cast_channel.CastMessage{
		SourceId:      proto.String("sender-0"),
		DestinationId: proto.String("receiver-0"),
		Namespace:     proto.String("urn:x-cast:com.google.cast.receiver"),
	}); err != nil {
		return err
	}

	msgChan := make(chan cast_channel.CastMessage)
	readErrChan := make(chan error)

	// TODO: verify this is not a forever-hanging goroutine
	go func() {
		for {
			msg, err := read(conn)
			if err != nil {
				readErrChan <- err
				return
			}
			msgChan <- msg
		}
	}()

	for {
		select {
		case <-done:
			return nil
		case err := <-readErrChan:
			return err
		case <-time.After(5 * time.Second):
			requestId++
			if err := send(conn, payloadHeaders{Type: "PING", RequestId: &requestId}, &cast_channel.CastMessage{
				SourceId:      proto.String("sender-0"),
				DestinationId: proto.String("receiver-0"),
				Namespace:     proto.String("urn:x-cast:com.google.cast.tp.heartbeat"),
			}); err != nil {
				return err
			}
		case message := <-msgChan:
			switch message.GetNamespace() {
			case "urn:x-cast:com.google.cast.tp.heartbeat":
				var headers payloadHeaders
				if err := json.Unmarshal([]byte(message.GetPayloadUtf8()), &headers); err != nil {
					return fmt.Errorf("Error unmarshaling JSON: %v", err)
				}
				if headers.Type == "PING" {
					if err := send(conn, payloadHeaders{Type: "PONG"}, &cast_channel.CastMessage{
						SourceId:      proto.String("sender-0"),
						DestinationId: proto.String("receiver-0"),
						Namespace:     proto.String("urn:x-cast:com.google.cast.tp.heartbeat"),
					}); err != nil {
						return err
					}
				} else if headers.Type == "PONG" {
					stateMu.Lock()
					if deviceType == chromecast {
						lastContact["chromecast"] = time.Now()
					} else if deviceType == chromecastAudio {
						lastContact["chromecastAudio"] = time.Now()
					}
					stateMu.Unlock()
				}
				break

			case "urn:x-cast:com.google.cast.receiver":
				type namespace struct {
					Name string `json:"name"`
				}
				type application struct {
					AppId       string      `json:"appId"`
					DisplayName string      `json:"displayName"`
					Namespaces  []namespace `json:"namespaces"`
					TransportId string      `json:"transportId"`
				}
				type receiverStatus struct {
					Applications []application `json:"applications"`
				}
				type receiverStatusPayload struct {
					Status receiverStatus `json:"status"`
				}
				var status receiverStatusPayload
				if err := json.Unmarshal([]byte(message.GetPayloadUtf8()), &status); err != nil {
					return fmt.Errorf("Error unmarshaling RECEIVER_STATUS JSON: %v", err)
				}
				log.Printf("status raw: %+v\n", message.GetPayloadUtf8())

				log.Printf("status = %+v\n", status)
				mediaFound := false
				for _, app := range status.Status.Applications {
					if app.AppId != "E8C28D3C" {
						log.Printf("Application %+v found, which is not the backdrop.\n", app)

						mediaFound = true

						stateMu.Lock()
						if deviceType == chromecast {
							//state.chromecastPlaying = true
						} else if deviceType == chromecastAudio {
							//state.chromecastAudioPlaying = true
						}
						stateMu.Unlock()
						stateChanged.Broadcast()
						break
					}
				}
				if !mediaFound {
					stateMu.Lock()
					if deviceType == chromecast {
						//state.chromecastPlaying = false
					} else if deviceType == chromecastAudio {
						//state.chromecastAudioPlaying = false
					}
					stateMu.Unlock()
					stateChanged.Broadcast()
				}
				break

			default:
				log.Printf("no handler for namespace %q\n", message.GetNamespace())
			}
		}
	}
}

func mdnsLookup(entriesCh chan *mdns.ServiceEntry) {
	params := mdns.DefaultParams(castService)
	params.Entries = entriesCh
	params.WantUnicastResponse = true
	mdns.Query(params)
}

// discoverAndPollChromecasts runs an infinite loop, discovering and polling
// chromecast devices in the local network for whether they are currently
// playing. The first discovered device is used.
func discoverAndPollChromecasts() {
	var chromecastsMu sync.RWMutex
	chromecasts := make(map[string]chan bool)
	entriesCh := make(chan *mdns.ServiceEntry, 5)

	go mdnsLookup(entriesCh)
	for {
		select {
		case entry := <-entriesCh:
			if !strings.Contains(entry.Name, castService) {
				continue
			}

			var deviceType chromecastDevice
			for _, field := range entry.InfoFields {
				if !strings.HasPrefix(field, "md=") {
					continue
				}
				if field == "md=Chromecast" {
					deviceType = chromecast
				} else if field == "md=Chromecast Audio" {
					deviceType = chromecastAudio
				}
			}
			hostport := fmt.Sprintf("%s:%d", entry.Addr, entry.Port)
			chromecastsMu.RLock()
			_, exists := chromecasts[hostport]
			chromecastsMu.RUnlock()
			if exists {
				continue
			}
			fmt.Printf("Found new chromecast at %q: %+v\n", hostport, entry)
			done := make(chan bool)
			chromecastsMu.Lock()
			chromecasts[hostport] = done
			chromecastsMu.Unlock()
			go func(deviceType chromecastDevice, done chan bool, hostport string) {
				err := pollChromecast(deviceType, done, hostport)
				if err != nil {
					log.Printf("Error polling chromecast %s:%d: %v\n", entry.Addr, entry.Port, err)
				}
				chromecastsMu.Lock()
				delete(chromecasts, hostport)
				chromecastsMu.Unlock()
			}(deviceType, done, hostport)
			stateMu.Lock()
			//state.chromecastPlaying = false
			stateMu.Unlock()
			stateChanged.Broadcast()

		case <-time.After(10 * time.Second):
			log.Printf("Starting new MDNS lookup\n")
			go mdnsLookup(entriesCh)
		}
	}
}
