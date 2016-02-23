package main

import (
	"bytes"
	"encoding/xml"
	"flag"
	"fmt"
	"net/http"
)

var (
	videoProjectorHomematicAddress = flag.String("video_projector_homematic_address",
		"MEQ1341845",
		"Homematic address (without colon, e.g. MEQ1341845) of a HM-ES-PMSw1-Pl to which the video projector is connected.")
)

const (
	switchStatePoweredOff = 0
	switchStatePoweredOn  = 1
)

// Returns the switch state of the video projector.
func getSwitchState() (int, error) {
	body := fmt.Sprintf(`<?xml version="1.0"?>
<methodCall>
   <methodName>getValue</methodName>
   <params>
      <param><value><string>%s:1</string></value></param>
      <param><value><string>STATE</string></value></param>
   </params>
</methodCall>`, *videoProjectorHomematicAddress)

	resp, err := http.Post("http://homematic-ccu2:2001/", "text/xml", bytes.NewReader([]byte(body)))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("Unexpected HTTP status: got %d (%v), want %d", resp.StatusCode, resp.Status, http.StatusOK)
	}
	var stateValueResponse struct {
		XMLName xml.Name `xml:"methodResponse"`
		State   bool     `xml:"params>param>value>boolean"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&stateValueResponse); err != nil {
		return 0, err
	}
	if stateValueResponse.State {
		return switchStatePoweredOn, nil
	} else {
		return switchStatePoweredOff, nil
	}
}

// Returns the power consumption of the video projector, in watts.
func getPowerConsumption() (float64, error) {
	body := fmt.Sprintf(`<?xml version="1.0"?>
<methodCall>
   <methodName>getValue</methodName>
   <params>
      <param><value><string>%s:2</string></value></param>
      <param><value><string>POWER</string></value></param>
   </params>
</methodCall>`, *videoProjectorHomematicAddress)

	resp, err := http.Post("http://homematic-ccu2:2001/", "text/xml", bytes.NewReader([]byte(body)))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("Unexpected HTTP status: got %d (%v), want %d", resp.StatusCode, resp.Status, http.StatusOK)
	}
	var powerValueResponse struct {
		XMLName xml.Name `xml:"methodResponse"`
		Power   float64  `xml:"params>param>value>double"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&powerValueResponse); err != nil {
		return 0, err
	}
	return powerValueResponse.Power, nil
}

func setSwitchState(state int) error {
	body := fmt.Sprintf(`<?xml version="1.0"?>
<methodCall>
   <methodName>setValue</methodName>
   <params>
      <param><value><string>%s:1</string></value></param>
      <param><value><string>STATE</string></value></param>
      <param><value><boolean>%d</boolean></value></param>
   </params>
</methodCall>`, *videoProjectorHomematicAddress, state)
	resp, err := http.Post("http://homematic-ccu2:2001/", "text/xml", bytes.NewReader([]byte(body)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Unexpected HTTP status: got %d (%v), want %d", resp.StatusCode, resp.Status, http.StatusOK)
	}
	return nil
}
