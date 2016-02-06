package main

import (
	"bytes"
	"encoding/xml"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	paramWhitelistStr = flag.String("param_whitelist",
		"",
		"If non-empty, only parameters that are specified in this comma-separated list will be pushed to Prometheus. E.g.: ACTUAL_TEMPERATURE,SET_TEMPERATURE")
	pushGateway = flag.String("prometheus_push_gateway",
		"http://pushgateway.zekjur.net:9091/",
		"URL of a https://github.com/prometheus/pushgateway instance")
	listenAddress = flag.String("listen_address",
		":1234",
		"host:port to listen on for XMLRPC requests. Will be registered at the CCU2")
	externalAddress = flag.String("external_address",
		"dr:1234",
		"host:port for the CCU2 to reach our XMLRPC server, see -listen_address")

	paramWhitelist = make(map[string]bool)
	gaugeDefs      = make(map[string]*prometheus.GaugeVec)
	lastEvent      = time.Now()
)

type structMember struct {
	XMLName xml.Name `xml:"member"`
	Name    string   `xml:"name"`
	Value   value    `xml:"value"`
}

type value struct {
	XMLName  xml.Name       `xml:"value"`
	String   string         `xml:"string,omitempty"`
	InnerXML string         `xml:",innerxml"`
	Array    []value        `xml:"array>data>value"`
	Struct   []structMember `xml:"struct>member"`
	Int      int32          `xml:"i4,omitempty"`
	Double   float64        `xml:"double,omitempty"`
	Bool     bool           `xml:"boolean,omitempty"`
}

type methodCall struct {
	XMLName    xml.Name `xml:"methodCall"`
	MethodName string   `xml:"methodName"`
	Params     []value  `xml:"params>param>value"`
}

func startXMLReply(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/xml")
	_, err := w.Write([]byte("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n"))
	return err
}

func handleListMethods(w http.ResponseWriter, r *http.Request, params []value) {
	// NB: The CCU2 doesn’t care at all about what we reply, as long as we
	// reply. I.e., the CCU2 will call methods which we do not list here.
	startXMLReply(w)
	xml.NewEncoder(w).Encode(&(struct {
		XMLName xml.Name `xml:"methodResponse"`
		Methods []value  `xml:"params>param>value>array>data>value"`
	}{
		Methods: []value{
			{String: "system.listMethods"},
			{String: "event"},
			{String: "listDevices"},
			{String: "newDevices"},
		},
	}))
}

func handleListDevices(w http.ResponseWriter, r *http.Request, params []value) {
	startXMLReply(w)
	xml.NewEncoder(w).Encode(&(struct {
		XMLName xml.Name `xml:"methodResponse"`
		Methods []value  `xml:"params>param>value>array>data>value"`
	}{}))
}

func handleMultiCall(w http.ResponseWriter, r *http.Request, params []value) {
	if got, want := len(params), 1; got != want {
		log.Fatalf("system.multicall has wrong number of parameters: got %d, want %d", got, want)
	}
	calls := params[0].Array
	for _, subcall := range calls {
		// Each subcall has a struct with methodName and params as members.
		if got, want := len(subcall.Struct), 2; got != want {
			log.Fatalf("system.multicall call has wrong number of struct members: got %d, want %d", got, want)
		}
		if got, want := subcall.Struct[0].Name, "methodName"; got != want {
			log.Fatalf("system.multicall struct member has wrong name: got %s, want %s", got, want)
		}
		if got, want := subcall.Struct[1].Name, "params"; got != want {
			log.Fatalf("system.multicall struct member has wrong name: got %s, want %s", got, want)
		}
		methodName := subcall.Struct[0].Value.InnerXML
		params := subcall.Struct[1].Value.Array
		dispatch(w, r, methodName, params)
	}
	startXMLReply(w)
	xml.NewEncoder(w).Encode(&(struct {
		XMLName xml.Name `xml:"methodResponse"`
		Methods []value  `xml:"params>param>value>array>data>value"`
	}{}))
}

func handleEvent(w http.ResponseWriter, r *http.Request, params []value) {
	if got, want := len(params), 4; got != want {
		log.Fatalf("Event had the wrong number of parameters: got %d, want %d", got, want)
	}
	address := params[1].InnerXML
	param := params[2].InnerXML
	var value float64
	// NOTE: I’d be interested in a cleaner solution, but AFAICT, encoding/xml
	// does not allow for a way to see which fields are set (but have a zero
	// value).
	innerXML := strings.ToLower(params[3].InnerXML)
	if strings.HasPrefix(innerXML, "<double>") {
		value = params[3].Double
	} else if strings.HasPrefix(innerXML, "<i4>") {
		value = float64(params[3].Int)
	} else if strings.HasPrefix(innerXML, "<boolean>") {
		if params[3].Bool {
			value = 1
		} else {
			value = 0
		}
	} else {
		log.Fatalf("Event value had no known type (innerXML=%q)", innerXML)
	}
	log.Printf("%q, %q, %f\n", address, param, value)
	if !paramWhitelist[param] {
		return
	}
	g, ok := gaugeDefs[param]
	if !ok {
		g = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "hm",
				Name:      param,
				Help:      "automatically defined from event"},
			[]string{"address"})
		prometheus.MustRegister(g)
		gaugeDefs[param] = g
	}
	g.With(prometheus.Labels{"address": address}).Set(value)
	lastEvent = time.Now()
}

func dispatch(w http.ResponseWriter, r *http.Request, methodName string, params []value) {
	if methodName == "system.listMethods" {
		handleListMethods(w, r, params)
	} else if methodName == "listDevices" {
		handleListDevices(w, r, params)
	} else if methodName == "system.multicall" {
		handleMultiCall(w, r, params)
	} else if methodName == "event" {
		handleEvent(w, r, params)
	}
}

func handleRpc2(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Invalid Method", http.StatusBadRequest)
		return
	}
	var call methodCall
	decoder := xml.NewDecoder(r.Body)
	if err := decoder.Decode(&call); err != nil {
		log.Printf("err=%v\n", err)
		return
	}
	dispatch(w, r, call.MethodName, call.Params)
}

func hmInit(url, local, registration string) error {
	// We avoid the methodCall type because it uses the value type, which
	// does not cleanly serialize (,omitempty does not work for structs,
	// see also http://stackoverflow.com/q/27246275/712014).
	type stringParam struct {
		XMLName xml.Name `xml:"param"`
		Value   string   `xml:"value>string"`
	}
	body, err := xml.Marshal(
		&(struct {
			XMLName    xml.Name      `xml:"methodCall"`
			MethodName string        `xml:"methodName"`
			Params     []stringParam `xml:"params>param"`
		}{
			MethodName: "init",
			Params: []stringParam{
				{Value: local},
				{Value: registration},
			},
		}))
	if err != nil {
		return err
	}

	request, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}

	request.Header.Set("Content-Type", "text/xml")
	request.Header.Set("Content-Length", fmt.Sprintf("%d", len(body)))

	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Unexpected HTTP Status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
	return nil
}

// Copied from src/net/http/server.go
type tcpKeepAliveListener struct {
	*net.TCPListener
}

func (ln tcpKeepAliveListener) Accept() (c net.Conn, err error) {
	tc, err := ln.AcceptTCP()
	if err != nil {
		return
	}
	tc.SetKeepAlive(true)
	tc.SetKeepAlivePeriod(3 * time.Minute)
	return tc, nil
}

func main() {
	flag.Parse()

	for _, param := range strings.Split(*paramWhitelistStr, ",") {
		paramWhitelist[param] = true
		log.Printf("Whitelisted parameter: %q\n", param)
	}

	http.HandleFunc("/", handleRpc2)
	http.Handle("/metrics", prometheus.Handler())

	listener, err := net.Listen("tcp", *listenAddress)
	if err != nil {
		log.Fatal(err)
	}

	srv := http.Server{Addr: *listenAddress}
	go srv.Serve(tcpKeepAliveListener{listener.(*net.TCPListener)})

	externalURL := "http://" + *externalAddress
	log.Printf("Listening on %q, registering %q at the CCU2\n", listener.Addr(), externalURL)

	if err := hmInit("http://homematic-ccu2:9292/groups", externalURL, "zkjhmserver"); err != nil {
		log.Fatal(err)
	}

	if err := hmInit("http://homematic-ccu2:2001/", externalURL, "zkjrfd"); err != nil {
		log.Fatal(err)
	}

	for {
		time.Sleep(1 * time.Minute)
		if time.Since(lastEvent) > 5*time.Minute {
			log.Fatalf("Last event received at %v (%v ago)", lastEvent, time.Since(lastEvent))
		}
		if err := prometheus.Push("hm", "hm", *pushGateway); err != nil {
			log.Fatal(err)
		}
	}

}
