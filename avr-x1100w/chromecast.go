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
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/hashicorp/mdns"
	"github.com/stapelberg/zkj-nas-tools/avr-x1100w/cast_channel"
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

func pollChromecast(addr net.IP, port int) error {
	var requestId int

	// TODO: use setreadtimeout?
	conn, err := tls.Dial("tcp", fmt.Sprintf("%s:%d", addr, port), &tls.Config{
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
					lastContact["chromecast"] = time.Now()
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

				// Connect to and request status of every currently running
				// application that can play media. If there are none,
				// chromecast is not playing currently.
				log.Printf("status = %+v\n", status)
				mediaFound := false
				for _, app := range status.Status.Applications {
					if app.AppId != "E8C28D3C" {
						log.Printf("Application %+v found, which is not the backdrop.\n", app)

						mediaFound = true

						stateMu.Lock()
						state.chromecastPlaying = true
						stateMu.Unlock()
						stateChanged.Broadcast()
						break
					}
					//for _, namespace := range app.Namespaces {
					//	if namespace.Name == "urn:x-cast:mdx-netflix-com:service:target:2" || namespace.Name == "urn:x-cast:com.google.cast.media" {
					//		// Since we cannot look into what netflix is doing, assume it is playing.
					//		mediaFound = true

					//		stateMu.Lock()
					//		state.chromecastPlaying = true
					//		stateMu.Unlock()
					//		stateChanged.Broadcast()
					//	}
					//	//if namespace.Name == "urn:x-cast:com.google.cast.media" {
					//	//	mediaFound = true

					//	//	requestId++
					//	//	if err := send(conn, payloadHeaders{Type: "CONNECT", RequestId: &requestId}, &cast_channel.CastMessage{
					//	//		SourceId:      proto.String("sender-0"),
					//	//		DestinationId: proto.String(app.TransportId),
					//	//		Namespace:     proto.String("urn:x-cast:com.google.cast.tp.connection"),
					//	//	}); err != nil {
					//	//		return err
					//	//	}

					//	//	requestId++
					//	//	if err := send(conn, payloadHeaders{Type: "GET_STATUS", RequestId: &requestId}, &cast_channel.CastMessage{
					//	//		SourceId:      proto.String("sender-0"),
					//	//		DestinationId: proto.String(app.TransportId),
					//	//		Namespace:     proto.String(namespace.Name),
					//	//	}); err != nil {
					//	//		return err
					//	//	}
					//	//}
					//}
				}
				if !mediaFound {
					stateMu.Lock()
					state.chromecastPlaying = false
					stateMu.Unlock()
					stateChanged.Broadcast()
				}
				break

			case "urn:x-cast:com.google.cast.media":
				type mediaStatus struct {
					PlayerState string `json:"playerState"`
				}
				type mediaStatusPayload struct {
					Status []mediaStatus `json:"status"`
				}

				var status mediaStatusPayload
				if err := json.Unmarshal([]byte(message.GetPayloadUtf8()), &status); err != nil {
					return fmt.Errorf("Error unmarshaling RECEIVER_STATUS JSON: %v", err)
				}

				playing := false
				for _, s := range status.Status {
					if s.PlayerState == "BUFFERING" || s.PlayerState == "PLAYING" {
						playing = true
					}
				}

				stateMu.Lock()
				state.chromecastPlaying = playing
				stateMu.Unlock()
				stateChanged.Broadcast()
				break

			default:
				log.Printf("no handler for namespace %q\n", message.GetNamespace())
			}
		}
	}
}

// discoverAndPollChromecasts runs an infinite loop, discovering and polling
// chromecast devices in the local network for whether they are currently
// playing. The first discovered device is used.
func discoverAndPollChromecasts() {
	entriesCh := make(chan *mdns.ServiceEntry)
	go mdns.Lookup(castService, entriesCh)
	for {
		select {
		case entry := <-entriesCh:
			if !strings.Contains(entry.Name, castService) {
				return
			}

			fmt.Printf("Got new chromecast: %v\n", entry)
			err := pollChromecast(entry.Addr, entry.Port)
			log.Printf("Error polling chromecast %s:%d: %v\n", entry.Addr, entry.Port, err)
			stateMu.Lock()
			state.chromecastPlaying = false
			stateMu.Unlock()
			stateChanged.Broadcast()
			break

		case <-time.After(10 * time.Second):
			log.Printf("Starting new MDNS lookup\n")
			go mdns.Lookup(castService, entriesCh)
			break
		}
	}
}
