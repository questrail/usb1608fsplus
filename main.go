// Copyright (c) 2016 The mccdaq developers. All rights reserved.
// Project site: https://github.com/gotmc/mccdaq
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"time"

	"github.com/gotmc/libusb"
	"github.com/gotmc/mccdaq/usb1608fsplus"
)

const millisecondDelay = 100

func main() {
	ctx, err := libusb.Init()
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

	// Print some info about the device
	log.Printf("Vendor ID = 0x%x / Product ID = 0x%x\n", daq.DeviceDescriptor.VendorID,
		daq.DeviceDescriptor.ProductID)
	serialNumber, err := daq.SerialNumber()
	log.Printf("Serial number via control transfer = %s", serialNumber)

	// Read the calibration memory to setup the gain table
	gainTable, _ := daq.BuildGainTable()
	log.Printf("Slope = %v\n", gainTable.Slope)
	log.Printf("Intercept = %v\n", gainTable.Intercept)

	/**************************
	* Start the Analog Scan   *
	**************************/

	// Create new analog input and ensure the scan is stopped and buffer cleared
	ai := daq.NewAnalogInput()
	ai.StopScan()
	time.Sleep(millisecondDelay * time.Millisecond)
	ai.ClearScanBuffer()

	// Setup the analog input scan
	configData, err := ioutil.ReadFile("./analog_config.json")
	if err != nil {
		log.Fatalf("Error reading the USB-1608FS-Plus JSON config file")
	}
	dec := json.NewDecoder(bytes.NewReader(configData))
	var configJSON = struct {
		*usb1608fsplus.AnalogInput `json:"analog_input"`
	}{
		ai,
	}
	if err := dec.Decode(&configJSON); err != nil {
		log.Fatalf("parse USB-1608FS-Plus: %v", err)
	}
	ai.SetScanRanges()
	log.Printf("Frequency = %f Hz", ai.Frequency)

	// Read the scan ranges
	time.Sleep(millisecondDelay * time.Millisecond)
	scanRanges, err := ai.ScanRanges()
	log.Printf("Ranges = %v\n", scanRanges)

	// Read the totalScans using splitScansIn number of scans
	const (
		scansPerBuffer = 256
		totalBuffers   = 10
	)
	expectedDuration := (scansPerBuffer * totalBuffers) / ai.Frequency
	ai.StartScan(0)
	start := time.Now()
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
			raw, err := usb1608fsplus.Volts(data[i*2:i*2+2], ai.Channels[i].Range)
			if err != nil {
				log.Printf("%s = 0x%02x%02x (Error: %s)\n", ai.Channels[i].Description, data[i*2+1], data[i*2], err)
			} else {
				log.Printf("%s = %.5f Vraw\n", ai.Channels[i].Description, raw)
			}
		}
	}
	elapsed := time.Since(start)
	log.Printf("Reading %d bytes took %.2f s", totalBytesRead, elapsed.Seconds())
	log.Printf("Anticipated reading %d bytes to take %.2f s",
		scansPerBuffer*totalBuffers*ai.NumEnabledChannels()*2, expectedDuration)
	// Stop the analog scan and close the DAQ
	time.Sleep(millisecondDelay * time.Millisecond)
	ai.StopScan()
	time.Sleep(millisecondDelay * time.Millisecond)
	daq.Close()
}
