// Copyright (c) 2016 The mccdaq developers. All rights reserved.
// Project site: https://github.com/gotmc/mccdaq
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"runtime"
	"time"

	"github.com/gizak/termui"
	"github.com/gotmc/mccdaq/usb1608fsplus"
	"github.com/mitchellh/go-homedir"
	rpi "github.com/nathan-osman/go-rpigpio"
)

const (
	millisecondDelay = 100
	termWidth        = 70
)

func main() {

	// Parse the config flags to determine the config JSON filename
	var (
		configFlag = flag.String("config", "./dashboard_config.json", "JSON config filename.")
	)
	flag.Parse()
	configFilename, err := homedir.Expand(*configFlag)
	if err != nil {
		log.Fatalln(err)
	}

	// If running from RPi, set GPIO3 low.
	if runtime.GOARCH == "arm" {
		gpio3, err := rpi.OpenPin(3, rpi.OUT)
		if err != nil {
			panic(err)
		}
		defer gpio3.Close()
		gpio3.Write(rpi.LOW)
	}

	// Initialize the USB Context
	ctx, err := usb1608fsplus.Init()
	if err != nil {
		log.Fatal("Couldn't create USB context. Ending now.")
	}
	defer ctx.Exit()

	// Find the first USB fevice with the VendorID and ProductID matching the MCC
	// USB-1608FS-Plus DAQ
	daq, err := usb1608fsplus.GetFirstDevice(ctx)
	if err != nil {
		log.Fatalf("Couldn't find a USB-1608FS-Plus: %s", err)
	}
	defer daq.Close()

	// Create new analog input and ensure the scan is stopped and buffer cleared
	ai, err := daq.NewAnalogInput()
	if err != nil {
		log.Fatalf("Error creating new analog input: %s", err)
	}
	ai.StopScan()
	time.Sleep(millisecondDelay * time.Millisecond)
	ai.ClearScanBuffer()

	// Initialize the terminal UI
	err = termui.Init()
	if err != nil {
		panic(err)
	}
	defer termui.Close()

	//termui.UseTheme("helloworld")

	// Setup list of info
	var infoStrings = make([]string, 5)
	serialNumber, err := daq.SerialNumber()
	if err != nil {
		serialNumber = "Unknown"
	}
	infoStrings[0] = fmt.Sprintf("S/N %s", serialNumber)
	infoList := termui.NewList()
	infoList.Items = infoStrings
	infoList.ItemFgColor = termui.ColorYellow
	infoList.BorderLabel = "USB-1608FS-Plus Info"
	infoList.Height = len(infoStrings) + 2
	infoList.Width = termWidth
	infoList.Y = 0

	var strs = make([]string, 6)

	ls := termui.NewList()
	ls.Items = strs
	ls.ItemFgColor = termui.ColorYellow
	ls.BorderLabel = "Analog Inputs"
	ls.Height = len(strs) + 2
	ls.Width = termWidth
	ls.Y = infoList.Y + infoList.Height

	par0 := termui.NewPar("Press q to quit")
	par0.Height = 1
	par0.Width = termWidth
	par0.Y = ls.Y + ls.Height
	par0.Border = false

	termui.Handle("/sys/kbd/q", func(termui.Event) {
		termui.StopLoop()
	})

	/**************************
	* Start the Analog Scan   *
	**************************/

	// Setup the analog input scan
	configData, err := ioutil.ReadFile(configFilename)
	if err != nil {
		log.Fatalf("Error reading the USB-1608FS-Plus JSON config file")
	}
	dec := json.NewDecoder(bytes.NewReader(configData))
	var configJSON = struct {
		ScansPerBuffer             int `json:"scans_per_buffer"`
		TotalBuffers               int `json:"total_buffers"`
		*usb1608fsplus.AnalogInput `json:"analog_input"`
	}{
		0,
		0,
		ai,
	}
	if err := dec.Decode(&configJSON); err != nil {
		log.Fatalf("parse USB-1608FS-Plus: %v", err)
	}
	scansPerBuffer := configJSON.ScansPerBuffer
	totalBuffers := configJSON.TotalBuffers
	ai.SetScanRanges()
	infoStrings[1] = fmt.Sprintf("Scans/buffer = %d", scansPerBuffer)
	infoStrings[2] = fmt.Sprintf("Total buffers = %d", totalBuffers)

	// Read the scan ranges
	time.Sleep(millisecondDelay * time.Millisecond)
	_, err = ai.ScanRanges()

	// Read the totalScans using splitScansIn number of scans
	ai.StartScan(0)
	totalBytesRead := 0
	for j := 0; j < totalBuffers; j++ {
		// time.Sleep(millisecondDelay * time.Millisecond)
		data, err := ai.ReadScan(scansPerBuffer)
		totalBytesRead += len(data)
		if err != nil {
			// Stop the analog scan and close the DAQ
			ai.StopScan()
			time.Sleep(millisecondDelay * time.Millisecond)
			daq.Close()
			log.Fatalf("Error reading scan: %s", err)
		}
		// Show the first value measured for each channel
		wordsToShow := 6
		for i := 0; i < wordsToShow; i++ {
			desiredByte := i * 2
			ch := ai.Channels[i]
			volts, err := ch.Volts(data[desiredByte : desiredByte+2])
			encodedValue := int(binary.LittleEndian.Uint16(data[desiredByte : desiredByte+2]))
			if err != nil {
				strs[i] = fmt.Sprintf("%5s = %d (Error: %s)\n",
					ch.Description, encodedValue, err)
			} else {
				strs[i] = fmt.Sprintf("[%6s](fg-red) = [%.5f V](fg-white) (%d / %#x) @ %srange\n",
					ch.Description, volts, encodedValue, encodedValue, ch.Range)
			}
		}
		infoStrings[4] = fmt.Sprintf("Frequency = %f Hz", ai.Frequency)
		infoStrings[3] = fmt.Sprintf("Bytes read = %d\n", totalBytesRead)
		termui.Render(infoList, ls, par0)
	}

	// Stop the analog scan and close the DAQ
	time.Sleep(millisecondDelay * time.Millisecond)
	ai.StopScan()
	time.Sleep(millisecondDelay * time.Millisecond)
	termui.Loop()
}
